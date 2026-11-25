package common

import (
	_ "embed"
	"errors"
	"strings"
	"sync"
)

var ErrPasswordTooCommon = errors.New("This password is too common. Choose a different one.")

// common_passwords.txt holds the 3000 most used passwords of at least
// MinAccountPasswordLength characters, lower-cased, one per line, taken from
// the NCSC "100k most used passwords" list.
//
//go:embed common_passwords.txt
var commonPasswordList string

var commonPasswords = sync.OnceValue(func() map[string]struct{} {
	passwords := make(map[string]struct{}, 3000)
	for line := range strings.SplitSeq(commonPasswordList, "\n") {
		if password := strings.TrimSpace(line); password != "" {
			passwords[password] = struct{}{}
		}
	}
	return passwords
})

// IsCommonPassword reports whether a password is on the common-password list,
// ignoring letter case.
func IsCommonPassword(password string) bool {
	_, found := commonPasswords()[strings.ToLower(password)]
	return found
}
