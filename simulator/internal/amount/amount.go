package amount

import (
	crand "crypto/rand"
	"errors"
	"math/big"
	"strings"
)

func ParseBKCToWei(raw string) (*big.Int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("amount is empty")
	}
	if strings.HasPrefix(raw, "-") || strings.HasPrefix(raw, "+") {
		return nil, errors.New("amount must be a positive decimal without sign")
	}
	whole, frac, hasFrac := strings.Cut(raw, ".")
	if whole == "" {
		whole = "0"
	}
	if !isDecimalDigits(whole) {
		return nil, errors.New("amount whole part must be decimal digits")
	}
	if hasFrac {
		if frac == "" || !isDecimalDigits(frac) {
			return nil, errors.New("amount fractional part must be decimal digits")
		}
		if len(frac) > 18 {
			return nil, errors.New("amount supports at most 18 decimal places")
		}
	} else {
		frac = ""
	}
	frac += strings.Repeat("0", 18-len(frac))
	combined := strings.TrimLeft(whole+frac, "0")
	if combined == "" {
		return nil, errors.New("amount must be greater than zero")
	}
	value, ok := new(big.Int).SetString(combined, 10)
	if !ok || value.Sign() <= 0 {
		return nil, errors.New("amount must be a positive decimal")
	}
	return value, nil
}

func isDecimalDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func RandomWeiInRange(minWei *big.Int, maxWei *big.Int) (*big.Int, error) {
	if minWei == nil || maxWei == nil || minWei.Sign() <= 0 || maxWei.Sign() <= 0 {
		return nil, errors.New("amount range must be positive")
	}
	if minWei.Cmp(maxWei) > 0 {
		return nil, errors.New("amount minimum exceeds maximum")
	}
	step, _ := ParseBKCToWei("0.01")
	minUnits := ceilDiv(minWei, step)
	maxUnits := new(big.Int).Div(maxWei, step)
	if minUnits.Cmp(maxUnits) > 0 {
		return randomBigIntInRange(minWei, maxWei)
	}
	unit, err := randomBigIntInRange(minUnits, maxUnits)
	if err != nil {
		return nil, err
	}
	return unit.Mul(unit, step), nil
}

func randomBigIntInRange(min *big.Int, max *big.Int) (*big.Int, error) {
	width := new(big.Int).Sub(max, min)
	width.Add(width, big.NewInt(1))
	offset, err := crand.Int(crand.Reader, width)
	if err != nil {
		return nil, err
	}
	return offset.Add(offset, min), nil
}

func ceilDiv(value *big.Int, divisor *big.Int) *big.Int {
	result := new(big.Int).Div(value, divisor)
	remainder := new(big.Int).Mod(value, divisor)
	if remainder.Sign() > 0 {
		result.Add(result, big.NewInt(1))
	}
	return result
}
