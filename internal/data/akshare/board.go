package akshare

import (
	"strings"

	"quantitative-trading/internal/types"
)

// MapBoard derives exchange board from a 6-digit code or ts_code.
// Returns ("", false) for BSE / unrecognized codes (caller should exclude).
func MapBoard(codeOrTS string) (types.Board, bool) {
	code := strings.ToUpper(strings.TrimSpace(codeOrTS))
	if i := strings.IndexByte(code, '.'); i >= 0 {
		code = code[:i]
	}
	if len(code) != 6 {
		return "", false
	}
	switch {
	case strings.HasPrefix(code, "8"), strings.HasPrefix(code, "4"), strings.HasPrefix(code, "92"):
		return types.BoardBSE, false // excluded
	case strings.HasPrefix(code, "688"), strings.HasPrefix(code, "689"):
		return types.BoardSTAR, true
	case strings.HasPrefix(code, "300"), strings.HasPrefix(code, "301"):
		return types.BoardChiNext, true
	case strings.HasPrefix(code, "60"):
		return types.BoardSSEMain, true
	case strings.HasPrefix(code, "000"), strings.HasPrefix(code, "001"),
		strings.HasPrefix(code, "002"), strings.HasPrefix(code, "003"):
		return types.BoardSZSEMain, true
	case strings.HasPrefix(code, "68"): // residual STAR-ish
		return types.BoardSTAR, true
	default:
		return "", false
	}
}

// ToTSCode maps a bare 6-digit code to ts_code (e.g. 600000.SH). Empty if excluded.
func ToTSCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if i := strings.IndexByte(code, '.'); i >= 0 {
		return code // already ts_code-like
	}
	board, ok := MapBoard(code)
	if !ok {
		return ""
	}
	switch board {
	case types.BoardSSEMain, types.BoardSTAR:
		return code + ".SH"
	case types.BoardSZSEMain, types.BoardChiNext:
		return code + ".SZ"
	default:
		return ""
	}
}

// IsBSE reports whether the code belongs to Beijing Stock Exchange.
func IsBSE(codeOrTS string) bool {
	code := strings.ToUpper(strings.TrimSpace(codeOrTS))
	if i := strings.IndexByte(code, '.'); i >= 0 {
		code = code[:i]
	}
	return strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") || strings.HasPrefix(code, "92")
}
