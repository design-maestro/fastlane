package domain

import (
	"fmt"
	"sort"
	"strings"
)

// ISO 3166-1 alpha-2 codes accepted by the country-routing setting.
// Keep this list explicit so invalid GeoIP tags are rejected before Xray is
// reconfigured. Names stay in LuCI, where the browser can localize them.
var supportedCountryCodes = map[string]struct{}{
	"AD": {}, "AE": {}, "AF": {}, "AG": {}, "AI": {}, "AL": {}, "AM": {}, "AO": {}, "AQ": {}, "AR": {}, "AS": {}, "AT": {}, "AU": {}, "AW": {}, "AX": {}, "AZ": {},
	"BA": {}, "BB": {}, "BD": {}, "BE": {}, "BF": {}, "BG": {}, "BH": {}, "BI": {}, "BJ": {}, "BL": {}, "BM": {}, "BN": {}, "BO": {}, "BQ": {}, "BR": {}, "BS": {}, "BT": {}, "BV": {}, "BW": {}, "BY": {}, "BZ": {},
	"CA": {}, "CC": {}, "CD": {}, "CF": {}, "CG": {}, "CH": {}, "CI": {}, "CK": {}, "CL": {}, "CM": {}, "CN": {}, "CO": {}, "CR": {}, "CU": {}, "CV": {}, "CW": {}, "CX": {}, "CY": {}, "CZ": {},
	"DE": {}, "DJ": {}, "DK": {}, "DM": {}, "DO": {}, "DZ": {}, "EC": {}, "EE": {}, "EG": {}, "EH": {}, "ER": {}, "ES": {}, "ET": {},
	"FI": {}, "FJ": {}, "FK": {}, "FM": {}, "FO": {}, "FR": {}, "GA": {}, "GB": {}, "GD": {}, "GE": {}, "GF": {}, "GG": {}, "GH": {}, "GI": {}, "GL": {}, "GM": {}, "GN": {}, "GP": {}, "GQ": {}, "GR": {}, "GS": {}, "GT": {}, "GU": {}, "GW": {}, "GY": {},
	"HK": {}, "HM": {}, "HN": {}, "HR": {}, "HT": {}, "HU": {}, "ID": {}, "IE": {}, "IL": {}, "IM": {}, "IN": {}, "IO": {}, "IQ": {}, "IR": {}, "IS": {}, "IT": {},
	"JE": {}, "JM": {}, "JO": {}, "JP": {}, "KE": {}, "KG": {}, "KH": {}, "KI": {}, "KM": {}, "KN": {}, "KP": {}, "KR": {}, "KW": {}, "KY": {}, "KZ": {},
	"LA": {}, "LB": {}, "LC": {}, "LI": {}, "LK": {}, "LR": {}, "LS": {}, "LT": {}, "LU": {}, "LV": {}, "LY": {},
	"MA": {}, "MC": {}, "MD": {}, "ME": {}, "MF": {}, "MG": {}, "MH": {}, "MK": {}, "ML": {}, "MM": {}, "MN": {}, "MO": {}, "MP": {}, "MQ": {}, "MR": {}, "MS": {}, "MT": {}, "MU": {}, "MV": {}, "MW": {}, "MX": {}, "MY": {}, "MZ": {},
	"NA": {}, "NC": {}, "NE": {}, "NF": {}, "NG": {}, "NI": {}, "NL": {}, "NO": {}, "NP": {}, "NR": {}, "NU": {}, "NZ": {},
	"OM": {}, "PA": {}, "PE": {}, "PF": {}, "PG": {}, "PH": {}, "PK": {}, "PL": {}, "PM": {}, "PN": {}, "PR": {}, "PS": {}, "PT": {}, "PW": {}, "PY": {}, "QA": {},
	"RE": {}, "RO": {}, "RS": {}, "RU": {}, "RW": {}, "SA": {}, "SB": {}, "SC": {}, "SD": {}, "SE": {}, "SG": {}, "SH": {}, "SI": {}, "SJ": {}, "SK": {}, "SL": {}, "SM": {}, "SN": {}, "SO": {}, "SR": {}, "SS": {}, "ST": {}, "SV": {}, "SX": {}, "SY": {}, "SZ": {},
	"TC": {}, "TD": {}, "TF": {}, "TG": {}, "TH": {}, "TJ": {}, "TK": {}, "TL": {}, "TM": {}, "TN": {}, "TO": {}, "TR": {}, "TT": {}, "TV": {}, "TW": {}, "TZ": {},
	"UA": {}, "UG": {}, "UM": {}, "US": {}, "UY": {}, "UZ": {}, "VA": {}, "VC": {}, "VE": {}, "VG": {}, "VI": {}, "VN": {}, "VU": {}, "WF": {}, "WS": {}, "YE": {}, "YT": {}, "ZA": {}, "ZM": {}, "ZW": {},
}

// SupportedCountryCodes returns the complete sorted ISO country catalog used
// by settings validation and the LuCI selector contract.
func SupportedCountryCodes() []string {
	codes := make([]string, 0, len(supportedCountryCodes))
	for code := range supportedCountryCodes {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// NormalizeCountryCode validates and canonicalizes an ISO country code.
func NormalizeCountryCode(value string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(value))
	if _, ok := supportedCountryCodes[code]; !ok {
		return "", fmt.Errorf("unsupported country code %q", value)
	}
	return code, nil
}

// CanonicalCountryRouting validates a complete country-routing preference.
func CanonicalCountryRouting(value CountryRouting) (CountryRouting, error) {
	if strings.TrimSpace(value.CountryCode) == "" {
		if value.Enabled {
			return CountryRouting{}, fmt.Errorf("country code is required when country routing is enabled")
		}
		return CountryRouting{}, nil
	}
	code, err := NormalizeCountryCode(value.CountryCode)
	if err != nil {
		return CountryRouting{}, err
	}
	value.CountryCode = code
	return value, nil
}
