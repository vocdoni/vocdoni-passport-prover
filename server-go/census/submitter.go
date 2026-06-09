package census

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ProofType constants from @zkpassport/utils
const (
	proofTypeDisclose = uint8(0)
	proofTypeBind     = uint8(8)
)

// ProofTypeLength[X].evm constants from @zkpassport/utils
const (
	discloseEvmLength = uint16(180) // 90 bytes discloseMask + 90 bytes disclosedBytes
	bindEvmLength     = uint16(509)
)

// BoundDataIdentifier.USER_ADDRESS from @zkpassport/utils
const boundDataUserAddress = uint8(1)

// DisclosureProof carries the committedInputs and param_commitment from one inner disclosure proof.
// This is populated from the mobile app's per-proof committedInputs field.
type DisclosureProof struct {
	CircuitName     string
	CommittedInputs map[string]any
	ParamCommitment string // publicInputs[4] of the inner proof, hex string
}

// BuildCommittedInputs serializes the committed inputs for all disclosure circuits and
// returns them concatenated in the order matching the outer proof's param_commitments.
//
// outerPublicInputs are the public inputs from the outer proof; param_commitments sit at
// indices [5..len-3) — one per disclosure circuit.
//
// Each disclosure's CommittedInputs map comes directly from the zkPassport mobile app
// (the "committedInputs" field on each inner disclosure proof).
func BuildCommittedInputs(disclosures []DisclosureProof, outerPublicInputs []string) ([]byte, error) {
	if len(outerPublicInputs) < 6 {
		return nil, fmt.Errorf("outer proof has too few public inputs (%d)", len(outerPublicInputs))
	}
	// param_commitments occupy indices 5..len-3 (exclusive), one per disclosure circuit.
	end := len(outerPublicInputs) - 3
	if end <= 5 {
		return nil, fmt.Errorf("no param_commitments found in outer public inputs (len=%d)", len(outerPublicInputs))
	}
	paramCommitments := outerPublicInputs[5:end]

	// For each param_commitment, find the matching disclosure by its inner publicInputs[4].
	var result []byte
	for _, pc := range paramCommitments {
		pcNorm := strings.ToLower(strings.TrimPrefix(pc, "0x"))
		var matched *DisclosureProof
		for i := range disclosures {
			inner := strings.ToLower(strings.TrimPrefix(disclosures[i].ParamCommitment, "0x"))
			if inner == pcNorm {
				matched = &disclosures[i]
				break
			}
		}
		if matched == nil {
			return nil, fmt.Errorf("no disclosure proof matches param_commitment %s", pc)
		}
		serialized, err := serializeCommittedInputs(matched.CircuitName, matched.CommittedInputs)
		if err != nil {
			return nil, fmt.Errorf("serialize committedInputs for %s: %w", matched.CircuitName, err)
		}
		result = append(result, serialized...)

		// Diagnostic: verify our serialized bytes hash to the expected param_commitment.
		h := sha256.Sum256(serialized)
		// param_commitment = sha256(bytes) >> 8: treat as big-endian, drop the last (least-significant) byte
		computed := make([]byte, 32)
		copy(computed[1:], h[:31]) // shift right by 8 bits: b0→pos1, b31 dropped, pos0=0
		computedHex := hex.EncodeToString(computed)
		pcNormFull := strings.ToLower(strings.TrimPrefix(pc, "0x"))
		match := computedHex == pcNormFull
		log.Printf("[census] param_commitment %s → %s (%d bytes) | sha256>>8=%s | match=%v | first32hex=%s",
			pc, matched.CircuitName, len(serialized), computedHex, match, hex.EncodeToString(serialized[:min(32, len(serialized))]))
	}
	return result, nil
}

// serializeCommittedInputs converts the per-circuit committedInputs map to the compact
// on-chain byte format expected by the IZKPassportVerifier contract.
//
// Format: [ProofType(1)] [length(2 BE)] [payload(length bytes)]
func serializeCommittedInputs(circuitName string, ci map[string]any) ([]byte, error) {
	switch circuitName {
	case "bind_evm":
		return serializeBindEvmInputs(ci)
	case "disclose_bytes_evm":
		return serializeDiscloseEvmInputs(ci)
	default:
		return nil, fmt.Errorf("unsupported disclosure circuit %q", circuitName)
	}
}

// serializeBindEvmInputs serializes { data: { user_address } } → 512 bytes.
// chain is not committed by the app (v1.0.5 binds address-only).
func serializeBindEvmInputs(ci map[string]any) ([]byte, error) {
	data, _ := ci["data"].(map[string]any)
	if data == nil {
		return nil, fmt.Errorf("bind_evm committedInputs missing 'data' object")
	}
	userAddress, _ := data["user_address"].(string)
	if userAddress == "" {
		return nil, fmt.Errorf("bind_evm committedInputs missing data.user_address")
	}
	return buildBindEvmCommittedInputs(userAddress)
}

// serializeDiscloseEvmInputs serializes { discloseMask: [...], disclosedBytes: [...] } → 183 bytes.
// When ci is empty (mobile app sends no committedInputs for zero-disclosure proofs), the mask and
// data are all zeros, which is the correct encoding when no fields are explicitly disclosed.
func serializeDiscloseEvmInputs(ci map[string]any) ([]byte, error) {
	buf := make([]byte, 1+2+int(discloseEvmLength)) // 183 bytes
	buf[0] = proofTypeDisclose
	binary.BigEndian.PutUint16(buf[1:3], discloseEvmLength)

	if len(ci) == 0 {
		// No disclosures: mask and disclosedBytes are all zeros.
		return buf, nil
	}

	maskRaw, _ := ci["discloseMask"].([]any)
	bytesRaw, _ := ci["disclosedBytes"].([]any)
	if len(maskRaw) != 90 {
		return nil, fmt.Errorf("disclose_bytes_evm discloseMask must be 90 bytes, got %d", len(maskRaw))
	}
	if len(bytesRaw) != 90 {
		return nil, fmt.Errorf("disclose_bytes_evm disclosedBytes must be 90 bytes, got %d", len(bytesRaw))
	}
	for i, v := range maskRaw {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("discloseMask[%d] is not a number", i)
		}
		buf[3+i] = byte(f)
	}
	for i, v := range bytesRaw {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("disclosedBytes[%d] is not a number", i)
		}
		buf[3+90+i] = byte(f)
	}
	return buf, nil
}

// validityPeriodInSeconds for the ZKPassport proof (24 hours).
const validityPeriodInSeconds = 86400

// Go structs matching the ProofVerificationParams ABI tuple layout.
// Field names are matched case-insensitively to ABI component names by go-ethereum.
type serviceConfigABI struct {
	ValidityPeriodInSeconds *big.Int
	Domain                  string
	Scope                   string
	DevMode                 bool
}

type proofVerificationDataABI struct {
	VkeyHash     [32]byte
	Proof        []byte
	PublicInputs [][32]byte
}

type proofVerificationParamsABI struct {
	Version               [32]byte
	ProofVerificationData proofVerificationDataABI
	CommittedInputs       []byte
	ServiceConfig         serviceConfigABI
}

// registerABIJSON is the ABI for register(address, ProofVerificationParams).
const registerABIJSON = `[{
    "type":"function",
    "name":"register",
    "inputs":[
        {"name":"account","type":"address"},
        {"name":"params","type":"tuple","components":[
            {"name":"version","type":"bytes32"},
            {"name":"proofVerificationData","type":"tuple","components":[
                {"name":"vkeyHash","type":"bytes32"},
                {"name":"proof","type":"bytes"},
                {"name":"publicInputs","type":"bytes32[]"}
            ]},
            {"name":"committedInputs","type":"bytes"},
            {"name":"serviceConfig","type":"tuple","components":[
                {"name":"validityPeriodInSeconds","type":"uint256"},
                {"name":"domain","type":"string"},
                {"name":"scope","type":"string"},
                {"name":"devMode","type":"bool"}
            ]}
        ]}
    ]
}]`

// Config holds the parameters needed to submit census registrations.
type Config struct {
	RPCURL          string
	PrivateKeyHex   string // funded wallet private key, hex without 0x prefix
	ContractAddress string // ZKPassportCensus contract address
	ChainID         int64  // e.g. 11155111 for Sepolia
	DevMode         bool   // pass devMode=true in ServiceConfig for test proofs
}

// Submitter sends register(address, ProofVerificationParams) transactions to ZKPassportCensus.
// The RootVerifier contract handles on-chain proof verification; no trusted backend required.
type Submitter struct {
	client   *ethclient.Client
	key      *ecdsa.PrivateKey
	from     common.Address
	contract common.Address
	chainID  *big.Int
	abi      abi.ABI
	devMode  bool

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

	parsedABI, err := abi.JSON(strings.NewReader(registerABIJSON))
	if err != nil {
		return nil, fmt.Errorf("parse register ABI: %w", err)
	}

	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, fmt.Errorf("fetch initial nonce: %w", err)
	}

	return &Submitter{
		client:   client,
		key:      privateKey,
		from:     from,
		contract: contract,
		chainID:  big.NewInt(cfg.ChainID),
		abi:      parsedABI,
		devMode:  cfg.DevMode,
		nonce:    nonce,
	}, nil
}

// Register submits a register(address, ProofVerificationParams) transaction to ZKPassportCensus.
//
//   - account:         voter's Ethereum address (hex, "0xABCD...")
//   - proof:           outer proof hex string from the prover-cli (with or without 0x)
//   - vkeyHash:        verification key hash hex string
//   - publicInputs:    hex-encoded bytes32 public inputs from the outer proof
//   - committedInputs: serialized committed inputs for all disclosure circuits (from BuildCommittedInputs)
//   - version:         version string from the aggregate response (e.g. "0.18.0")
//   - scope:           service scope string (e.g. "vocdoni")
//   - domain:          service domain (e.g. "passport.vocdoni.io")
func (s *Submitter) Register(
	ctx context.Context,
	account, proof, vkeyHash string,
	publicInputs []string,
	committedInputs []byte,
	version, scope, domain string,
) (string, error) {
	addr := common.HexToAddress(account)

	versionBytes, err := encodeVersion(version)
	if err != nil {
		return "", fmt.Errorf("encode version: %w", err)
	}

	vkeyHashBytes, err := decodeBytes32(vkeyHash)
	if err != nil {
		return "", fmt.Errorf("decode vkeyHash: %w", err)
	}

	proofBytes, err := decodeHex(proof)
	if err != nil {
		return "", fmt.Errorf("decode proof: %w", err)
	}

	pi, err := decodePublicInputs(publicInputs)
	if err != nil {
		return "", fmt.Errorf("decode public inputs: %w", err)
	}

	params := proofVerificationParamsABI{
		Version: versionBytes,
		ProofVerificationData: proofVerificationDataABI{
			VkeyHash:     vkeyHashBytes,
			Proof:        proofBytes,
			PublicInputs: pi,
		},
		CommittedInputs: committedInputs,
		ServiceConfig: serviceConfigABI{
			ValidityPeriodInSeconds: big.NewInt(validityPeriodInSeconds),
			Domain:                  domain,
			Scope:                   scope,
			DevMode:                 s.devMode,
		},
	}

	callData, err := s.abi.Pack("register", addr, params)
	if err != nil {
		return "", fmt.Errorf("abi pack: %w", err)
	}

	gasTipCap, err := s.client.SuggestGasTipCap(ctx)
	if err != nil {
		return "", fmt.Errorf("suggest gas tip cap: %w", err)
	}
	header, err := s.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("fetch latest header: %w", err)
	}
	gasFeeCap := new(big.Int).Add(gasTipCap, new(big.Int).Mul(header.BaseFee, big.NewInt(2)))

	// ZKPassport RootVerifier + on-chain proof verification: allow ~1.5M gas.
	const verifyGasLimit = 1_500_000

	s.nonceMu.Lock()
	nonce := s.nonce

	rawTx := &types.DynamicFeeTx{
		ChainID:   s.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       verifyGasLimit,
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

// encodeVersion converts "0.18.0" to a bytes32 version identifier.
// Each component is encoded as 2 big-endian bytes, right-padded to 32 bytes.
func encodeVersion(version string) ([32]byte, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return [32]byte{}, fmt.Errorf("invalid version %q: expected major.minor.patch", version)
	}
	var result [32]byte
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return [32]byte{}, fmt.Errorf("invalid version component %q: %w", p, err)
		}
		result[i*2] = byte(n >> 8)
		result[i*2+1] = byte(n)
	}
	return result, nil
}

// buildBindEvmCommittedInputs constructs the 512-byte committedInputs for a bind_evm proof.
// Layout: [ProofType.BIND (1)] [length=509 (2)] [formatBoundData right-padded to 509 bytes]
// App v1.0.5 binds address-only — no CHAIN_ID TLV is included.
func buildBindEvmCommittedInputs(signerAddress string) ([]byte, error) {
	addrHex := strings.TrimPrefix(signerAddress, "0x")
	if len(addrHex) < 40 {
		addrHex = strings.Repeat("0", 40-len(addrHex)) + addrHex
	}
	if len(addrHex) != 40 {
		return nil, fmt.Errorf("invalid address %q", signerAddress)
	}
	addrBytes, err := hex.DecodeString(addrHex)
	if err != nil {
		return nil, fmt.Errorf("decode address: %w", err)
	}

	// TLV-encoded bound data (matches @zkpassport/utils formatBoundData)
	// USER_ADDRESS: [0x01, 0x00, 0x14, ...20 bytes]
	var data []byte
	data = append(data, boundDataUserAddress, 0x00, byte(len(addrBytes)))
	data = append(data, addrBytes...)

	// Right-pad to bindEvmLength (509) bytes
	payload := make([]byte, bindEvmLength)
	copy(payload, data)

	// Prepend header: [ProofType.BIND] [length (2 bytes big-endian)]
	result := make([]byte, 3+int(bindEvmLength))
	result[0] = proofTypeBind
	binary.BigEndian.PutUint16(result[1:3], bindEvmLength)
	copy(result[3:], payload)

	return result, nil
}

// decodeHex decodes a hex string (with or without 0x prefix) to bytes.
func decodeHex(h string) ([]byte, error) {
	h = strings.TrimPrefix(h, "0x")
	b, err := hex.DecodeString(h)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// decodeBytes32 decodes a hex string to a [32]byte, left-padding if needed.
func decodeBytes32(h string) ([32]byte, error) {
	h = strings.TrimPrefix(h, "0x")
	if len(h) < 64 {
		h = strings.Repeat("0", 64-len(h)) + h
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		return [32]byte{}, err
	}
	if len(b) > 32 {
		return [32]byte{}, fmt.Errorf("value exceeds 32 bytes (%d bytes)", len(b))
	}
	var result [32]byte
	copy(result[32-len(b):], b)
	return result, nil
}

// decodePublicInputs converts hex strings to [][32]byte for ABI encoding.
func decodePublicInputs(inputs []string) ([][32]byte, error) {
	result := make([][32]byte, len(inputs))
	for i, s := range inputs {
		b32, err := decodeBytes32(s)
		if err != nil {
			return nil, fmt.Errorf("public input [%d]: %w", i, err)
		}
		result[i] = b32
	}
	return result, nil
}
