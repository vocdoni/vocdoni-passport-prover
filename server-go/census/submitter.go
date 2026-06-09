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

// registerABIJSON is the ABI for TrustedCensus.register(address,uint256).
const registerABIJSON = `[{
    "type":"function",
    "name":"register",
    "inputs":[
        {"name":"account","type":"address"},
        {"name":"nullifier","type":"uint256"}
    ]
}]`

// Config holds the parameters needed to submit census registrations.
type Config struct {
	RPCURL        string
	PrivateKeyHex string // funded wallet private key, hex without 0x prefix
	ChainID       int64  // e.g. 11155111 for Sepolia
}

// Submitter sends register(address, uint256) transactions to TrustedCensus.
// The backend verifies the zkPassport outer proof off-chain; no on-chain proof
// verification is needed, so gas usage is significantly lower.
type Submitter struct {
	client  *ethclient.Client
	key     *ecdsa.PrivateKey
	from    common.Address
	chainID *big.Int
	abi     abi.ABI

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

	parsedABI, err := abi.JSON(strings.NewReader(registerABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse register ABI: %w", err)
	}

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("fetch initial nonce: %w", err)
	}

	return &Submitter{
		client:  client,
		key:     privateKey,
		from:    from,
		chainID: big.NewInt(cfg.ChainID),
		abi:     parsedABI,
		nonce:   nonce,
	}, nil
}

// Register submits a register(address, uint256) transaction to TrustedCensus.
//
//   - account:         voter's Ethereum address (hex, "0xABCD...")
//   - nullifier:       scoped nullifier from the outer proof public inputs (hex string "0x...")
//   - contractAddress: per-election TrustedCensus address; falls back to the default
//     address set in Config when empty.
func (s *Submitter) Register(ctx context.Context, account, nullifier, contractAddress string) (string, error) {
	addr := common.HexToAddress(account)

	nullifierInt, err := parseNullifier(nullifier)
	if err != nil {
		return "", fmt.Errorf("parse nullifier: %w", err)
	}

	callData, err := s.abi.Pack("register", addr, nullifierInt)
	if err != nil {
		return "", fmt.Errorf("abi pack: %w", err)
	}

	contractAddress = strings.TrimSpace(contractAddress)
	if contractAddress == "" {
		return "", fmt.Errorf("censusContract not provided in request payload")
	}
	contract := common.HexToAddress(contractAddress)

	gasTipCap, err := s.client.SuggestGasTipCap(ctx)
	if err != nil {
		return "", fmt.Errorf("suggest gas tip cap: %w", err)
	}
	header, err := s.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("fetch latest header: %w", err)
	}
	gasFeeCap := new(big.Int).Add(gasTipCap, new(big.Int).Mul(header.BaseFee, big.NewInt(2)))

	// TrustedCensus.register: LeanIMT insert only, no on-chain proof verification.
	const registerGasLimit = 200_000

	s.nonceMu.Lock()
	nonce := s.nonce

	rawTx := &types.DynamicFeeTx{
		ChainID:   s.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       registerGasLimit,
		To:        &contract,
		Data:      callData,
	}

	signer := types.NewLondonSigner(s.chainID)
	signedTx, err := types.SignNewTx(s.key, signer, rawTx)
	if err != nil {
		s.nonceMu.Unlock()
		return "", fmt.Errorf("sign tx: %w", err)
	}

	if sendErr := s.client.SendTransaction(ctx, signedTx); sendErr != nil {
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

// FromAddress returns the hex address of the funded wallet.
func (s *Submitter) FromAddress() string {
	return s.from.Hex()
}

// parseNullifier converts a hex string ("0x..." or bare hex) to *big.Int.
func parseNullifier(h string) (*big.Int, error) {
	h = strings.TrimPrefix(h, "0x")
	n, ok := new(big.Int).SetString(h, 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex: %q", h)
	}
	return n, nil
}
