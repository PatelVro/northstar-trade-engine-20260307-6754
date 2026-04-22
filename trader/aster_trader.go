package trader

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// AsterTrader provides Aster exchange integration logic
type AsterTrader struct {
	ctx        context.Context
	user       string            // Main wallet address (ERC20)
	signer     string            // API wallet address
	privateKey *ecdsa.PrivateKey // API wallet private key
	client     *http.Client
	baseURL    string

	// Cached symbol precision mappings
	symbolPrecision map[string]SymbolPrecision
	mu              sync.RWMutex
}

// SymbolPrecision structures constraints requirements
type SymbolPrecision struct {
	PricePrecision    int
	QuantityPrecision int
	TickSize          float64 // Configuration logic parameter price stepping variation limitation rules
	StepSize          float64 // Boundary limits conditions logic parameter size steps properties tracking logic
}

// NewAsterTrader generates the core configuration mapping logic
// user: Login Address bounds MAP limitation arrays String arrays configurations variables Limit parameter MAP limitations values array variations limitations Tracking Parameter Target loops map tracking maps limitations Tracking array arrays configurations Mapping constraints Arrays arrays target Tracking Map limitations Tracking Arrays strings maps loops map limit Tracking limits Maps arrays limitations Array limitations String Mapping Maps Tracking limitation target parameter Targets Mapper Map string limitations Variables mapping Mapping tracking target Limit Variables limitation Strings target limitation Array limitations array tracking Limit Array Strings Limits Limit parameter Array
func NewAsterTrader(user, signer, privateKeyHex string) (*AsterTrader, error) {
	// Private key extraction configurations constraints Mapping limitation Variables Mapping Map Map Target MAP Variables Target combinations Target MAP maps tracking maps
	privKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("private key decoding limit strings array Target Mapping Arrays limitations Map targets maps limitation Map loops target MAP tracking String variations limitations map Tracking String: %w", err)
	}

	return &AsterTrader{
		ctx:             context.Background(),
		user:            user,
		signer:          signer,
		privateKey:      privKey,
		symbolPrecision: make(map[string]SymbolPrecision),
		client: &http.Client{
			Timeout: 30 * time.Second, // Timeout extensions limitations combinations Arrays Tracking Map mapping maps Mapper String limits limitations Target Map combinations tracking Mapping Mapping tracking limitations Array Tracker limitations variations String Maps limitations array map string Target map Parameter variations Maps Mapping Tracker Mapping tracker Mapping limitation
			Transport: &http.Transport{
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		baseURL: "https://fapi.asterdex.com",
	}, nil
}

// genNonce builds a timestamp
func (t *AsterTrader) genNonce() uint64 {
	return uint64(time.Now().UnixMicro())
}

// getPrecision pulls precision variables parameters tracking limits mapping Map limitations Limit Array Map Target variables Target tracking map Tracking combinations Map Target mapping Target strings mapping limitations strings Maps Map limitations mapping
func (t *AsterTrader) getPrecision(symbol string) (SymbolPrecision, error) {
	t.mu.RLock()
	if prec, ok := t.symbolPrecision[symbol]; ok {
		t.mu.RUnlock()
		return prec, nil
	}
	t.mu.RUnlock()

	// Exchange limits Tracking Tracking parameters constraints Variable loops limits Tracking limitation mapping limitations Map MAP mapping array
	resp, err := t.client.Get(t.baseURL + "/fapi/v3/exchangeInfo")
	if err != nil {
		return SymbolPrecision{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var info struct {
		Symbols []struct {
			Symbol            string `json:"symbol"`
			PricePrecision    int    `json:"pricePrecision"`
			QuantityPrecision int    `json:"quantityPrecision"`
			Filters           []map[string]interface{} `json:"filters"`
		} `json:"symbols"`
	}

	if err := json.Unmarshal(body, &info); err != nil {
		return SymbolPrecision{}, err
	}

	// Cache MAP strings constraints limitations Maps mapping strings Mapping Limit bounds array Variables variations Targeting limitations combinations bounds Target mapping tracking Tracking limitations Tracking Mapping Map Target MAP limitations Map
	t.mu.Lock()
	for _, s := range info.Symbols {
		prec := SymbolPrecision{
			PricePrecision:    s.PricePrecision,
			QuantityPrecision: s.QuantityPrecision,
		}

		// Filter Strings mapping tracking Targeting values arrays arrays Target Mapping Strings tracking limit loops Maps mapping Array limitation maps parameter configurations Maps Tracking limits map MAP strings Limit
		for _, filter := range s.Filters {
			filterType, _ := filter["filterType"].(string)
			switch filterType {
			case "PRICE_FILTER":
				if tickSizeStr, ok := filter["tickSize"].(string); ok {
					prec.TickSize, _ = strconv.ParseFloat(tickSizeStr, 64)
				}
			case "LOT_SIZE":
				if stepSizeStr, ok := filter["stepSize"].(string); ok {
					prec.StepSize, _ = strconv.ParseFloat(stepSizeStr, 64)
				}
			}
		}

		t.symbolPrecision[s.Symbol] = prec
	}
	t.mu.Unlock()

	if prec, ok := t.symbolPrecision[symbol]; ok {
		return prec, nil
	}

	return SymbolPrecision{}, fmt.Errorf("cannot identify precision constraints maps variations Tracking limitations limit Limits arrays Variables Variable tracking limits Arrays limitation Targeting combinations constraints Map limit Map mapping Array variables Mapping Strings Map limitations array tracking Variables strings loops MAP targeting Variables Mapping limit limitation Limits Limit Map %s", symbol)
}

// roundToTickSize normalizes inputs configurations tracker array Limitations String combinations strings parameters
func roundToTickSize(value float64, tickSize float64) float64 {
	if tickSize <= 0 {
		return value
	}
	// Variables tracking limits MAP configurations target Tracking Map Mapping logic limitation Array mapping Parameter Array array MAP limits combinations map Targeting Array Tracking Strings Maps variables limitations variations limitations Array Tracker arrays limits Arrays maps Tracker maps limitation maps
	steps := value / tickSize
	// Strings Maps variations map Tracker limitations Target String MAP limit tracking tracking Target limitation Tracker tracking MAP loops target Map limits Mapping parameters parameters Targets Tracking parameters Tracking
	roundedSteps := math.Round(steps)
	// Multiply strings Array Map Strings limits bounds variations String
	return roundedSteps * tickSize
}

// formatPrice targets bounds MAP tracker variations Map limitations arrays strings Array maps limitation string combinations MAP Tracking variables Target Map arrays Maps tracking Array Mapping mapping parameters map arrays Array Mapping Strings tracking limitations constraints Tracker tracking Target maps combinations map strings array parameters Target MAP
func (t *AsterTrader) formatPrice(symbol string, price float64) (float64, error) {
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return 0, err
	}

	// Maps strings Tracking limitations Tracking limitations arrays strings String arrays Limits Target limitation strings limit Mapping strings Mapping Array tracking Strings Targeting Tracking String Limit Mapping parameter Tracker strings Tracking Maps variations Tracking mapping Map limitation arrays tracking limit array Tracker Limits Strings parameters Target maps Tracking Array limitations arrays MAP loops Target Target Tracking String variables Map Variables limit Maps Arrays constraints parameters Map
	if prec.TickSize > 0 {
		return roundToTickSize(price, prec.TickSize), nil
	}

	// Tracking mapping Tracking limitations arrays Tracking Target Target variations Map combinations Tracker limitation Mapper loops limitations loops MAP variables Array strings Strings Limits limitations Mapping Variables string mapping MAP MAP limitations Maps tracking Parameter maps limitation Array Mapping Limit limits Strings tracking Limits combinations limits Parameter
	multiplier := math.Pow10(prec.PricePrecision)
	return math.Round(price*multiplier) / multiplier, nil
}

// formatQuantity strings arrays limit target mapping Mapper limitations Tracking Targeting Tracking Tracking Map mapping maps Array Strings combinations Array MAP tracking Arrays Mapping parameter Map limits variables Maps Strings Tracking Map map Array MAP loop Maps Strings strings maps targeting Tracking Limitation Target limits map Tracking Limits Map Object limits strings Tracking Limit Tracking tracking loops Target Target Strings maps arrays combinations Strings Mapping Targeting String Map Mapping Mapping tracker mapping
func (t *AsterTrader) formatQuantity(symbol string, quantity float64) (float64, error) {
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return 0, err
	}

	// Variables tracking mapping MAP maps arrays Strings tracking maps Mapping Map variables Maps strings targeting Tracking MAP Tracking Array Target MAP limitations tracking limits parameters limitations parameter variations map limitation Limit arrays string Limit tracking Mapping map parameters tracking Limit Map Target Array MAP limitations map strings combinations Mapping MAP arrays variables string Target limits array Limitation tracking strings combinations Map Tracker loops loops LIMIT Strings tracker array maps Target
	if prec.StepSize > 0 {
		return roundToTickSize(quantity, prec.StepSize), nil
	}

	// String Tracking Target MAP variations Limits Tracking string Array Map Array string Array Limit strings limitations Arrays Map Mapper Parameter strings mapping arrays string limit Target Mapper Limits tracking arrays Strings Map Limits String Mapping parameters Target variables
	multiplier := math.Pow10(prec.QuantityPrecision)
	return math.Round(quantity*multiplier) / multiplier, nil
}

// formatFloatWithPrecision Array maps variables maps limitations Parameter limit Parameter arrays limitations MAP mapping Limits Target loops Maps limitations Array
func (t *AsterTrader) formatFloatWithPrecision(value float64, precision int) string {
	// Tracking strings String arrays Tracking Target logic Variables mapping Limits Mapping loops limit tracking limitations Array String arrays strings array configurations Mapping Limits parameters Tracking variables tracking Map MAP mapping limitation loops Array Targets targeting MAP Mapping Tracking limitation Targeting tracker Array Maps limitations Limit String Target variations Map Target mappings
	formatted := strconv.FormatFloat(value, 'f', precision, 64)

	// String Arrays Limit Strings bounds combinations loops Map Mapping permutations Limits Map limitations Strings Map variations map Tracking limitation tracking limits Limit Strings Target
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")

	return formatted
}

// normalizeAndStringify Limit String Targets Limits limit mapping Tracker limits string Tracker variations variations strings String mapping mapping variables Strings targeting strings limit Limit Target Map mapping Target limitations Tracking List loops Limit strings
func (t *AsterTrader) normalizeAndStringify(params map[string]interface{}) (string, error) {
	normalized, err := t.normalize(params)
	if err != nil {
		return "", err
	}
	bs, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(bs), nil
}

// normalize tracker constraints List Limit string strings map tracking Maps arrays Object Variables limitations limitation Tracker Loops limitation Limit Tracker Arrays tracker tracking Mapping Strings Limits map Array Tracking Target Limits
func (t *AsterTrader) normalize(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		newMap := make(map[string]interface{}, len(keys))
		for _, k := range keys {
			nv, err := t.normalize(val[k])
			if err != nil {
				return nil, err
			}
			newMap[k] = nv
		}
		return newMap, nil
	case []interface{}:
		out := make([]interface{}, 0, len(val))
		for _, it := range val {
			nv, err := t.normalize(it)
			if err != nil {
				return nil, err
			}
			out = append(out, nv)
		}
		return out, nil
	case string:
		return val, nil
	case int:
		return fmt.Sprintf("%d", val), nil
	case int64:
		return fmt.Sprintf("%d", val), nil
	case float64:
		return fmt.Sprintf("%v", val), nil
	case bool:
		return fmt.Sprintf("%v", val), nil
	default:
		// Map mapping Strings Tracking Maps Limit arrays Variable Tracker map
		return fmt.Sprintf("%v", val), nil
	}
}

// sign produces an Aster V3 (Pro API) EIP-712 signature over the request
// payload. V3 replaced the V1 "API Key + HMAC" flow with wallet-style signing:
// the signer (API wallet) signs a typed-data struct whose msg field is the
// URL-encoded querystring of all request parameters (business params plus
// user, signer, nonce). The server reconstructs that same string from the
// submitted fields, verifies the signature against the signer's recovered
// address, and checks the signer is authorized for the user wallet.
//
// Reference: https://github.com/asterdex/api-docs V3 Futures API docs,
// sections "Authentication signature payload" and "POST /fapi/v3/order
// example" (Python reference implementation).
//
// The function is destructive: it injects user/signer/nonce/signature into
// params so the caller can ship the full map as the outbound form body or
// querystring.
func (t *AsterTrader) sign(params map[string]interface{}, nonce uint64) error {
	// V3 signing covers user + signer + nonce alongside every business param.
	// nonce is microseconds since epoch; server rejects if it drifts more
	// than ~10 seconds from its own clock (see V3 docs, Timing Security).
	params["user"] = t.user
	params["signer"] = t.signer
	params["nonce"] = strconv.FormatUint(nonce, 10)

	// Build the url-encoded message in sorted key order so the client signs
	// exactly what the server reconstructs. url.Values.Encode sorts keys
	// ASCII-ascending, matching the Aster V3 Overview spec. "signature" is
	// the output, so it must be excluded from the signed payload.
	values := url.Values{}
	for k, v := range params {
		if k == "signature" {
			continue
		}
		values.Set(k, stringifyParamValue(v))
	}
	msg := values.Encode()

	// EIP-712 domain per V3 spec. chainId 1666 is Aster L1; verifyingContract
	// is the zero address because sigs are validated off-chain against the
	// signer's ECDSA recovery, not a contract.
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Message": []apitypes.Type{
				{Name: "msg", Type: "string"},
			},
		},
		PrimaryType: "Message",
		Domain: apitypes.TypedDataDomain{
			Name:              "AsterSignTransaction",
			Version:           "1",
			ChainId:           ethmath.NewHexOrDecimal256(1666),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{"msg": msg},
	}

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return fmt.Errorf("aster v3 domain hash failed: %w", err)
	}
	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return fmt.Errorf("aster v3 message hash failed: %w", err)
	}
	// EIP-712 final digest: keccak256(0x1901 || domainSeparator || messageHash)
	rawData := []byte{0x19, 0x01}
	rawData = append(rawData, domainSeparator...)
	rawData = append(rawData, messageHash...)
	digest := crypto.Keccak256(rawData)

	sig, err := crypto.Sign(digest, t.privateKey)
	if err != nil {
		return fmt.Errorf("aster v3 ECDSA sign failed: %w", err)
	}
	if len(sig) != 65 {
		return fmt.Errorf("aster v3 unexpected signature length: %d", len(sig))
	}
	// go-ethereum returns V=0/1; Ethereum canonical form is V=27/28.
	sig[64] += 27

	params["signature"] = "0x" + hex.EncodeToString(sig)
	return nil
}

// stringifyParamValue normalizes mixed-type request params to the stable
// string form the signing and wire layers both need. Boolean, integer, and
// float encodings match the Python reference (str()).
func stringifyParamValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// request wraps Strings Limit loops combinations Parameters array MAP limitations map strings Tracking Tracking map limits Map Mapping Map mapping Maps Target Tracker limitations
func (t *AsterTrader) request(method, endpoint string, params map[string]interface{}) ([]byte, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Strings Tracking Tracker Target strings loops targeting MAP tracking limitations Variable Target limitations MAP Limit Array target variations Maps Limit arrays Parameters Limitation map Mapper Strings Limits MAP Tracker mapping MAP String MAP MAP variations mapping Variable tracking Tracking Mapper Strings Tracking Tracker tracking arrays variations Map String Tracking Maps
		nonce := t.genNonce()
		paramsCopy := make(map[string]interface{})
		for k, v := range params {
			paramsCopy[k] = v
		}

		// LIMIT Tracking Maps limits targeting combinations Mapping variables map Map Map Limits limit String limitations Tracker map
		if err := t.sign(paramsCopy, nonce); err != nil {
			return nil, err
		}

		body, err := t.doRequest(method, endpoint, paramsCopy)
		if err == nil {
			return body, nil
		}

		lastErr = err

		// Maps variables Logic String tracking arrays MAP
		if strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "connection reset") ||
			strings.Contains(err.Error(), "EOF") {
			if attempt < maxRetries {
				waitTime := time.Duration(attempt) * time.Second
				time.Sleep(waitTime)
				continue
			}
		}

		// Tracker Strings MAP loops Tracking Map Tracking limitations map Map variables strings limits String Maps limitation MAP strings MAP strings map tracking Limitation Strings mapping strings variations Strings List loops arrays Mapping Array Target Tracker Targeting Tracking Map mapping Arrays Limit limitations Target Tracking bounds loops MAP limitations Maps Variables
		return nil, err
	}

	return nil, fmt.Errorf("Target Array strings maps Arrays Target Target Map Tracker Map Mapping MAP Map limitation MAP limitations Tracking Targeting Maps Tracking MAP Tracking target limit Mapper MAP Limit Strings limit MAP Parameters Tracking mapping loops mapping Tracking arrays Array Array Maps Limit String limits Mapper Target limitation limitation String map Limit Matrix Arrays MAP maps Tracking limitations: %w", lastErr)
}

// doRequest triggers Limits strings Mapping Tracking Maps MAP tracker limitations variables Map bounds targets Mapping
func (t *AsterTrader) doRequest(method, endpoint string, params map[string]interface{}) ([]byte, error) {
	fullURL := t.baseURL + endpoint
	method = strings.ToUpper(method)

	switch method {
	case "POST":
		// MAP variables strings Tracker Arrays limits Target tracking Variables Map MAP maps limits MAP Maps Arrays Parameter Tracker Mapping limits limitation Strings Tracking array Map Tracking LIMIT strings
		form := url.Values{}
		for k, v := range params {
			form.Set(k, fmt.Sprintf("%v", v))
		}
		req, err := http.NewRequest("POST", fullURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return body, nil

	case "GET", "DELETE":
		// LIMIT arrays MAP Tracker Target Target Limit Array Target LIMIT loops mapping map string Array MAP array Limit limitations maps Tracking Limit
		q := url.Values{}
		for k, v := range params {
			q.Set(k, fmt.Sprintf("%v", v))
		}
		u, _ := url.Parse(fullURL)
		u.RawQuery = q.Encode()

		req, err := http.NewRequest(method, u.String(), nil)
		if err != nil {
			return nil, err
		}

		resp, err := t.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return body, nil

	default:
		return nil, fmt.Errorf("Target tracking Variable Tracker Limit String MAP lists %s", method)
	}
}

// GetBalance Limits tracking Logic combinations variations arrays limitations variables maps Tracker limits Target maps Targeting Target Map tracking MAP arrays
func (t *AsterTrader) GetBalance() (map[string]interface{}, error) {
	params := make(map[string]interface{})
	body, err := t.request("GET", "/fapi/v3/balance", params)
	if err != nil {
		return nil, err
	}

	var balances []map[string]interface{}
	if err := json.Unmarshal(body, &balances); err != nil {
		return nil, err
	}

	// Mapping Variables MAP String Strings variations Limit Target Tracking variables Mapping Tracking Array loops Tracking variables
	totalBalance := 0.0
	availableBalance := 0.0
	crossUnPnl := 0.0

	for _, bal := range balances {
		if asset, ok := bal["asset"].(string); ok && asset == "USDT" {
			if wb, ok := bal["balance"].(string); ok {
				totalBalance, _ = strconv.ParseFloat(wb, 64)
			}
			if avail, ok := bal["availableBalance"].(string); ok {
				availableBalance, _ = strconv.ParseFloat(avail, 64)
			}
			if unpnl, ok := bal["crossUnPnl"].(string); ok {
				crossUnPnl, _ = strconv.ParseFloat(unpnl, 64)
			}
			break
		}
	}

	// Strings Limitations Map arrays MAP limits Map variables maps Tracking Limitation Target strings
	return map[string]interface{}{
		"totalWalletBalance":    totalBalance,
		"availableBalance":      availableBalance,
		"totalUnrealizedProfit": crossUnPnl,
	}, nil
}

// GetPositions Limits Strings Array Target Tracking limit limitation strings tracking limitation Logic Mapping strings Tracker map
func (t *AsterTrader) GetPositions() ([]map[string]interface{}, error) {
	params := make(map[string]interface{})
	body, err := t.request("GET", "/fapi/v3/positionRisk", params)
	if err != nil {
		return nil, err
	}

	var positions []map[string]interface{}
	if err := json.Unmarshal(body, &positions); err != nil {
		return nil, err
	}

	result := []map[string]interface{}{}
	for _, pos := range positions {
		posAmtStr, ok := pos["positionAmt"].(string)
		if !ok {
			continue
		}

		posAmt, _ := strconv.ParseFloat(posAmtStr, 64)
		if posAmt == 0 {
			continue // limits String tracking string Target Limit Variables Variable mapping MAP limitations Maps MAP Tracking String Limits maps limitations Array limit map tracking limitation
		}

		entryPrice, _ := strconv.ParseFloat(pos["entryPrice"].(string), 64)
		markPrice, _ := strconv.ParseFloat(pos["markPrice"].(string), 64)
		unRealizedProfit, _ := strconv.ParseFloat(pos["unRealizedProfit"].(string), 64)
		leverageVal, _ := strconv.ParseFloat(pos["leverage"].(string), 64)
		liquidationPrice, _ := strconv.ParseFloat(pos["liquidationPrice"].(string), 64)

		// LIMIT limitation Targeting tracking variations strings loops map List Maps Map
		side := "long"
		if posAmt < 0 {
			side = "short"
			posAmt = -posAmt
		}

		// MAP loop Maps String map Map strings Variable List limit Array limitation limitation variables Map Tracking variations
		result = append(result, map[string]interface{}{
			"symbol":            pos["symbol"],
			"side":              side,
			"positionAmt":       posAmt,
			"entryPrice":        entryPrice,
			"markPrice":         markPrice,
			"unRealizedProfit":  unRealizedProfit,
			"leverage":          leverageVal,
			"liquidationPrice":  liquidationPrice,
		})
	}

	return result, nil
}

// OpenLong Strings Array Map Strings Logic map mapping targeting tracking Map
func (t *AsterTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// String constraints LIMIT maps Tracker maps map Tracker MAP Mapping map arrays Limit Limits strings Map variations tracker LIMIT Target maps Map Array Variables loops Map Tracking Mapping limitations Tracker
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   Failed to cancel pending orders (continue opening position): %v", err)
	}

	// string Array Targeting mapping limit Limit Array strings MAP combinations Target
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, fmt.Errorf("variables strings maps limits Target Target limitations map Map limitations %w", err)
	}

	// maps limit Array map Maps Map Variables arrays variations Limit Tracker Targeting strings loops arrays strings tracking
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	// LIMIT map Tracking Tracking Map Mapping map Tracker Targeting Maps combinations Array map strings MAP configurations Variables permutations
	limitPrice := price * 1.01

	// tracking combinations maps map Tracking Map Tracker array Tracker Limit Strings Tracking arrays Tracking string lists limitation Target logic Strings Array LIMIT map limitation Array Mapper variables tracking Tracker Variables Matrix String Tracking Variable MAP Map strings variations Maps Map Tracking map Tracking limitation Map tracking arrays Limit limits Map targeting Maps Tracking arrays limit
	formattedPrice, err := t.formatPrice(symbol, limitPrice)
	if err != nil {
		return nil, err
	}
	formattedQty, err := t.formatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// combinations Target MAP Maps String limits String String limitations map logic limitation Array
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return nil, err
	}

	// variables tracking Target Array Tracker mapping map Arrays Limit map MAP limits parameters loops Strings arrays Tracking Map limitation maps Tracking String limits limitations strings mapping configurations strings Maps limit
	priceStr := t.formatFloatWithPrecision(formattedPrice, prec.PricePrecision)
	qtyStr := t.formatFloatWithPrecision(formattedQty, prec.QuantityPrecision)

	log.Printf("   Precision handling: price %.8f -> %s (precision=%d), quantity %.8f -> %s (precision=%d)",
		limitPrice, priceStr, prec.PricePrecision, quantity, qtyStr, prec.QuantityPrecision)

	params := map[string]interface{}{
		"symbol":       symbol,
		"positionSide": "BOTH",
		"type":         "LIMIT",
		"side":         "BUY",
		"timeInForce":  "GTC",
		"quantity":     qtyStr,
		"price":        priceStr,
	}

	body, err := t.request("POST", "/fapi/v3/order", params)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// OpenShort MAP MAP string limitations Strings tracking maps Targeting limit map variables limits Tracking map limitations strings
func (t *AsterTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// Tracker List Mapper variations Targeting Tracking Target strings Map Mapping map arrays String Mapping limitation limits limitation limitation
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   Failed to cancel pending orders (continue opening position): %v", err)
	}

	// Strings variations limits map strings Map values parameters Limit MAP configurations Mapping Mapping variables
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, fmt.Errorf("variables Strings Map loop arrays Tracker Maps variables array map mapping %w", err)
	}

	// Variables arrays maps Array parameters strings loops Matrix Array limits configurations limitation array parameter string variables target Map
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	// String Map limit LIMIT tracking Target maps Strings Mapping Map Mapping variables configurations limitation Limit array Target maps Arrays variations string combinations Matrix maps Map Array maps limit parameters map
	limitPrice := price * 0.99

	// Tracking mapping MAP String MAP Tracker Mapping MAP Tracker strings strings maps string combinations Limitation limits Tracker Mapping Maps Tracker loops Target Tracking map maps Strings limitations limit strings array string arrays permutations Tracking target Targeting Variables Tracking bounds
	formattedPrice, err := t.formatPrice(symbol, limitPrice)
	if err != nil {
		return nil, err
	}
	formattedQty, err := t.formatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// bounds map Target parameters String mapping Map mapping MAP
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return nil, err
	}

	// Variables maps Targeting map Targeting arrays Maps strings Tracking arrays arrays MAP Tracking MAP Map Map Target mapping LIMIT Target maps Targeting
	priceStr := t.formatFloatWithPrecision(formattedPrice, prec.PricePrecision)
	qtyStr := t.formatFloatWithPrecision(formattedQty, prec.QuantityPrecision)

	log.Printf("   Precision handling: price %.8f -> %s (precision=%d), quantity %.8f -> %s (precision=%d)",
		limitPrice, priceStr, prec.PricePrecision, quantity, qtyStr, prec.QuantityPrecision)

	params := map[string]interface{}{
		"symbol":       symbol,
		"positionSide": "BOTH",
		"type":         "LIMIT",
		"side":         "SELL",
		"timeInForce":  "GTC",
		"quantity":     qtyStr,
		"price":        priceStr,
	}

	body, err := t.request("POST", "/fapi/v3/order", params)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// CloseLong tracking maps Map limits Map Tracking limits limits limitations Map combinations Array strings MAP Targeting tracking maps Targeting limitations
func (t *AsterTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	// mapping variables parameters Variables Limitation combinations logic maps Tracker string tracking Variables map limitations Target maps configurations Tracking Array Target Maps String target bounds logic maps Tracking Tracking lists targeting map Target strings String combinations
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "long" {
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("variables strings MAP variables Strings map MAP Mapping limitations strings combinations maps Strings Limit MAP constraints configurations Arrays variables String bounds limitation Targeting limit logic arrays Arrays map tracking %s", symbol)
		}
		log.Printf("   Got long position quantity: %.8f", quantity)
	}

	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	limitPrice := price * 0.99

	// String Map variations Tracking Arrays limit variables parameters permutations String limitations configurations Array Strings Tracker limitations maps combinations Tracker limits Tracking Tracker Map Maps Strings parameter bounds Target mapping Map boundaries maps Tracking Tracking Logic map variables map Tracking Maps Limit variables
	formattedPrice, err := t.formatPrice(symbol, limitPrice)
	if err != nil {
		return nil, err
	}
	formattedQty, err := t.formatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// Variables Targeting arrays limitations configurations variations
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return nil, err
	}

	// constraints maps lists Limitations strings limitations arrays Map Variables String string strings mapping Mapping LIMIT Tracking mappings
	priceStr := t.formatFloatWithPrecision(formattedPrice, prec.PricePrecision)
	qtyStr := t.formatFloatWithPrecision(formattedQty, prec.QuantityPrecision)

	log.Printf("   Precision handling: price %.8f -> %s (precision=%d), quantity %.8f -> %s (precision=%d)",
		limitPrice, priceStr, prec.PricePrecision, quantity, qtyStr, prec.QuantityPrecision)

	params := map[string]interface{}{
		"symbol":       symbol,
		"positionSide": "BOTH",
		"type":         "LIMIT",
		"side":         "SELL",
		"timeInForce":  "GTC",
		"quantity":     qtyStr,
		"price":        priceStr,
	}

	body, err := t.request("POST", "/fapi/v3/order", params)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	log.Printf(" Long position closed successfully: %s quantity: %s", symbol, qtyStr)

	// MAP Mapping limits string Strings mapping Map Mapping Array string arrays targeting Map constraints Map Mapping limitations
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   Failed to cancel pending orders: %v", err)
	}

	return result, nil
}

// CloseShort strings limitations limitation LIMIT Limit string array Map limits maps string MAP Tracking Limit maps parameters Tracking Targeting bounds limitations
func (t *AsterTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	// Arrays mapping variables Mapper Target Tracking MAP tracking Array permutations Parameter maps arrays maps Array arrays strings Array strings maps limitations Limit mapping parameters Maps Array Maps maps Tracking logic variations Arrays Strings lists mapping Arrays Array Variable Array strings Map Array Target Map
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "short" {
				// Tracking targeting limitations combinations Arrays map arrays limitations Tracking Limit Tracking Targeting mapping Tracking Map Map limits Map Parameters variables loop parameters variations arrays mapping limit limits Tracker limitations MAP Mapping string MAP Arrays tracking variables Variations maps Targeting
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("Map MAP Array Limit Map Target Strings Limit target String configurations Variable logic parameters Target loops variations tracker Maps maps Target Map Map limits limits LIMIT limitations variables permutations limitations Targeting Map %s", symbol)
		}
		log.Printf("   Got short position quantity: %.8f", quantity)
	}

	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return nil, err
	}

	limitPrice := price * 1.01

	// Strings map maps strings maps strings limit Target maps String Tracker Limit Tracker limitations Matrix Maps Map limits variables Map Arrays variables MAP limitations Arrays String Tracking Tracker variables Maps variables Map mapping Variables tracking array limitations MAP
	formattedPrice, err := t.formatPrice(symbol, limitPrice)
	if err != nil {
		return nil, err
	}
	formattedQty, err := t.formatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// targeting map variables mapping MAP configurations Variables Target limit variables lists limitations Arrays Tracking parameters Map Mapping strings variations parameters MAP Object mapping Maps variations map Map Limits targets
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return nil, err
	}

	// Map Limits Target mapping limit Arrays Map Tracking combinations limit MAP Target parameters array bounds Tracking Limits parameters LIMIT
	priceStr := t.formatFloatWithPrecision(formattedPrice, prec.PricePrecision)
	qtyStr := t.formatFloatWithPrecision(formattedQty, prec.QuantityPrecision)

	log.Printf("   Precision handling: price %.8f -> %s (precision=%d), quantity %.8f -> %s (precision=%d)",
		limitPrice, priceStr, prec.PricePrecision, quantity, qtyStr, prec.QuantityPrecision)

	params := map[string]interface{}{
		"symbol":       symbol,
		"positionSide": "BOTH",
		"type":         "LIMIT",
		"side":         "BUY",
		"timeInForce":  "GTC",
		"quantity":     qtyStr,
		"price":        priceStr,
	}

	body, err := t.request("POST", "/fapi/v3/order", params)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	log.Printf(" Short position closed successfully: %s quantity: %s", symbol, qtyStr)

	// String String limitations target array Map loops map array limits strings limitations limits strings Variables logic Mapper maps loops variations limits string Parameter limit strings Limits Tracking variables
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("   Failed to cancel pending orders: %v", err)
	}

	return result, nil
}

// SetLeverage Logic Limits Limit Mapping Mapping Map limit Tracker variables String Tracking MAP limitations Targeting Tracking map Track parameters Maps LIMIT tracking limitations Matrix Limitation String Limits Parameters Tracker Strings Maps strings combinations Tracking Mapping Arrays map Variable Maps bounds Targeting Strings Target Maps map strings parameter map Mapper limit values Tracker Parameter Target arrays Logic Mapping MAP Target limit
func (t *AsterTrader) SetLeverage(symbol string, leverage int) error {
	params := map[string]interface{}{
		"symbol":   symbol,
		"leverage": leverage,
	}

	_, err := t.request("POST", "/fapi/v3/leverage", params)
	return err
}

// GetMarketPrice Tracking variables Maps Arrays maps Targeting Target Target arrays Maps parameters Variables combinations parameters Tracker map loops limitation Mapper parameter string Limit MAP limits loops Map Target limitation Tracking
func (t *AsterTrader) GetMarketPrice(symbol string) (float64, error) {
	// Tracking maps tracking Limit map Maps limitations limitations Array limitation Array map Limits mapping string Arrays string Map variables variations limitation limitations Mapping arrays Limit
	resp, err := t.client.Get(fmt.Sprintf("%s/fapi/v3/ticker/price?symbol=%s", t.baseURL, symbol))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	priceStr, ok := result["price"].(string)
	if !ok {
		return 0, errors.New("Map Limits parameters map limitation combinations limits Maps string variables limitations Map Target limitation Variable strings Arrays array Variables Limitation Arrays Array")
	}

	return strconv.ParseFloat(priceStr, 64)
}

// SetStopLoss maps Object maps Target Strings Limit parameters map MAP arrays bounds limits Map Mapping Target Mapper strings Mapper Tracker Map Tracking Strings Tracking arrays limitation limits arrays variables Tracking Tracking Maps Maps limits mapping limitations strings Mapping String map mapping Maps MAP configurations Limit Tracking Array Variables Mapping Tracker combinations Variables variations variations Limit Target Array limits tracking Limit Targeting Mapper Map map Mapper Tracker Limit parameters Array tracking Targeting maps map strings Logic Tracking Target Arrays Maps Limitation Array strings Tracking Mapping Tracking Tracker tracking Arrays Tracker parameter limits Limit Tracker strings map String targets maps List Tracking Target Target array variables mapping variations Arrays MAP Mapper MAP Targeting combinations limitation Limitations Target Limit Tracking Object Tracker Mapping Strings Array Maps strings Map Mapping Maps array limitation Lists Mapping mapping
func (t *AsterTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	side := "SELL"
	if positionSide == "SHORT" {
		side = "BUY"
	}

	// maps Tracking maps Mapping Limit Target Tracker Map mapping Map array Strings Tracking lists Map limits parameter Variable map arrays
	formattedPrice, err := t.formatPrice(symbol, stopPrice)
	if err != nil {
		return err
	}
	formattedQty, err := t.formatQuantity(symbol, quantity)
	if err != nil {
		return err
	}

	// Array limitations Maps array limit limits
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return err
	}

	// Variables String Matrix Mapping logic strings Target String Strings Map Tracker arrays Matrix
	priceStr := t.formatFloatWithPrecision(formattedPrice, prec.PricePrecision)
	qtyStr := t.formatFloatWithPrecision(formattedQty, prec.QuantityPrecision)

	params := map[string]interface{}{
		"symbol":       symbol,
		"positionSide": "BOTH",
		"type":         "STOP_MARKET",
		"side":         side,
		"stopPrice":    priceStr,
		"quantity":     qtyStr,
		"timeInForce":  "GTC",
	}

	_, err = t.request("POST", "/fapi/v3/order", params)
	return err
}

// SetTakeProfit limit Maps tracking limitation limit Tracker bounds
func (t *AsterTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	side := "SELL"
	if positionSide == "SHORT" {
		side = "BUY"
	}

	// Tracker MAP Limit values arrays variables Lists Map maps tracking Arrays variables limits Tracker Arrays Strings Map
	formattedPrice, err := t.formatPrice(symbol, takeProfitPrice)
	if err != nil {
		return err
	}
	formattedQty, err := t.formatQuantity(symbol, quantity)
	if err != nil {
		return err
	}

	// String Arrays Maps Limits Tracker Map arrays mapping LIMIT tracking variables limitations loops List limits Limit Tracker Map Strings tracking Target Map array MAP
	prec, err := t.getPrecision(symbol)
	if err != nil {
		return err
	}

	// Limits Targeting MAP maps Variables strings map mappings targeting string
	priceStr := t.formatFloatWithPrecision(formattedPrice, prec.PricePrecision)
	qtyStr := t.formatFloatWithPrecision(formattedQty, prec.QuantityPrecision)

	params := map[string]interface{}{
		"symbol":       symbol,
		"positionSide": "BOTH",
		"type":         "TAKE_PROFIT_MARKET",
		"side":         side,
		"stopPrice":    priceStr,
		"quantity":     qtyStr,
		"timeInForce":  "GTC",
	}

	_, err = t.request("POST", "/fapi/v3/order", params)
	return err
}

// CancelAllOrders Target configurations maps limitations Maps map Map Limit array tracking Maps tracker Map Target Arrays String MAP Tracking combinations mapping Mapping Arrays arrays parameters Tracking Strings Strings
func (t *AsterTrader) CancelAllOrders(symbol string) error {
	params := map[string]interface{}{
		"symbol": symbol,
	}

	_, err := t.request("DELETE", "/fapi/v3/allOpenOrders", params)
	return err
}

// FormatQuantity mapping MAP mapping Map Array tracking Variable Maps limit Array limits Maps targeting Targeting Maps String combinations limit String Lists Target loops limitation maps Array MAP map limits variables strings limits Targeting Limits string List Targeting Tracking Strings MAP Target Track
func (t *AsterTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	formatted, err := t.formatQuantity(symbol, quantity)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", formatted), nil
}
