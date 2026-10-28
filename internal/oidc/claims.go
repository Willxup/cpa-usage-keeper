package oidc

import (
	"bytes"
	"encoding/json"
	"math"
	"time"
)

// validClaims 补齐库的应用边界：精确 issuer、只信任本客户端、必需时间与 nonce 类型。
func validClaims(claims map[string]json.RawMessage, issuer, clientID, nonce string, now time.Time) bool {
	if optionalString(claims, "iss") != issuer || optionalString(claims, "sub") == "" || optionalString(claims, "nonce") != nonce {
		return false
	}
	var audience string
	if json.Unmarshal(claims["aud"], &audience) != nil {
		var audiences []string
		if json.Unmarshal(claims["aud"], &audiences) != nil || len(audiences) != 1 {
			return false
		}
		audience = audiences[0]
	}
	if audience != clientID {
		return false
	}
	if raw, present := claims["azp"]; present {
		var authorizedParty string
		if json.Unmarshal(raw, &authorizedParty) != nil || authorizedParty != clientID {
			return false
		}
	}
	exp, validExp := numericDate(claims["exp"])
	iat, validIat := numericDate(claims["iat"])
	if !validExp || !validIat || !exp.After(now.Add(-ClockSkew)) || iat.After(now.Add(ClockSkew)) || !exp.After(iat) {
		return false
	}
	if raw, present := claims["nbf"]; present {
		nbf, valid := numericDate(raw)
		if !valid || nbf.After(now.Add(ClockSkew)) {
			return false
		}
	}
	if raw, present := claims["at_hash"]; present {
		var hash string
		if json.Unmarshal(raw, &hash) != nil || hash == "" {
			return false
		}
	}
	return true
}

func numericDate(raw json.RawMessage) (time.Time, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return time.Time{}, false
	}
	number, ok := value.(json.Number)
	if !ok {
		return time.Time{}, false
	}
	seconds, err := number.Float64()
	if err != nil || math.IsInf(seconds, 0) || seconds < -62135596800 || seconds > 253402300799 {
		return time.Time{}, false
	}
	whole, fractional := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fractional*1e9)), true
}

func optionalString(claims map[string]json.RawMessage, name string) string {
	var value string
	_ = json.Unmarshal(claims[name], &value)
	return value
}
