package census

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Config holds the parameters needed to submit census registrations.
type Config struct {
	RPCURL          string
	PrivateKeyHex   string // funded wallet private key, hex without 0x prefix
	ContractAddress string // TrustedCensus contract address on Sepolia
	ChainID         int64  // 11155111 for Sepolia
}

// Submitter sends register(address,uint256) transactions to a TrustedCensus contract.
type Submitter struct {
	client   *ethclient.Client
	key      *ecdsa.PrivateKey
	from     common.Address
	contract common.Address
	chainID  *big.Int
	args     abi.Arguments // (address, uint256) for ABI encoding
	selector [4]byte       // keccak256("register(address,uint256)")[0:4]

	nonceMu sync.Mutex
	nonce   uint64
}

// NewSubmitter dials the RPC, loads the private key, and fetches the initial pending nonce.
func NewSubmitter(ctx context.Context, cfg Config) (*Submitter, error) {
	client, err := ethclient.DialContext(ctx, cfg.RPCURL)
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}

	keyHex := strings.TrimPrefix(cfg.PrivateKeyHex, "0x")
	privateKey, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)

	contract := common.HexToAddress(cfg.ContractAddress)
	chainID := big.NewInt(cfg.ChainID)

	// Build ABI argument types for register(address, uint256)
	addressTy, err := abi.NewType("address", "", nil)
	if err != nil {
		return nil, fmt.Errorf("build address abi type: %w", err)
	}
	uint256Ty, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, fmt.Errorf("build uint256 abi type: %w", err)
	}
	args := abi.Arguments{
		{Type: addressTy},
		{Type: uint256Ty},
	}

	// Function selector: keccak256("register(address,uint256)")[0:4]
	sig := crypto.Keccak256([]byte("register(address,uint256)"))
	var selector [4]byte
	copy(selector[:], sig[:4])

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("fetch initial nonce: %w", err)
	}

	return &Submitter{
		client:   client,
		key:      privateKey,
		from:     from,
		contract: contract,
		chainID:  chainID,
		args:     args,
		selector: selector,
		nonce:    nonce,
	}, nil
}

// Register submits a register(address, nullifier) transaction to the census contract.
// address is the voter's Ethereum address ("0x..."), nullifier is a hex field element.
// Returns the transaction hash on success.
func (s *Submitter) Register(ctx context.Context, address, nullifier string) (string, error) {
	addr := common.HexToAddress(address)

	nullifierInt, ok := new(big.Int).SetString(strings.TrimPrefix(nullifier, "0x"), 16)
	if !ok {
		return "", fmt.Errorf("parse nullifier %q", nullifier)
	}

	packed, err := s.args.Pack(addr, nullifierInt)
	if err != nil {
		return "", fmt.Errorf("abi pack: %w", err)
	}
	callData := append(s.selector[:], packed...)

	gasTipCap, err := s.client.SuggestGasTipCap(ctx)
	if err != nil {
		return "", fmt.Errorf("suggest gas tip cap: %w", err)
	}
	// base fee headroom: tip + 2x base fee is a common heuristic
	header, err := s.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("fetch latest header: %w", err)
	}
	gasFeeCap := new(big.Int).Add(gasTipCap, new(big.Int).Mul(header.BaseFee, big.NewInt(2)))

	s.nonceMu.Lock()
	nonce := s.nonce

	rawTx := &types.DynamicFeeTx{
		ChainID:   s.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       200_000,
		To:        &s.contract,
		Data:      callData,
	}

	signer := types.NewLondonSigner(s.chainID)
	signedTx, err := types.SignNewTx(s.key, signer, rawTx)
	if err != nil {
		s.nonceMu.Unlock()
		return "", fmt.Errorf("sign tx: %w", err)
	}

	if sendErr := s.client.SendTransaction(ctx, signedTx); sendErr != nil {
		// Refresh nonce from chain so next call uses a valid nonce
		if refreshed, refreshErr := s.client.PendingNonceAt(ctx, s.from); refreshErr == nil {
			s.nonce = refreshed
		}
		s.nonceMu.Unlock()
		return "", fmt.Errorf("send tx: %w", sendErr)
	}
	s.nonce++
	s.nonceMu.Unlock()

	return signedTx.Hash().Hex(), nil
}

// FromAddress returns the address of the funded wallet.
func (s *Submitter) FromAddress() string {
	return s.from.Hex()
}
