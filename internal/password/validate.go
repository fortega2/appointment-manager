package password

import "unicode/utf8"

const (
	// MinLength is the shortest password accepted, per OWASP ASVS.
	MinLength = 12
	// MaxLength is the longest password accepted. It is a sanity bound: Argon2's
	// cost comes from its parameters, not from the length of its input.
	MaxLength = 128
)

// Validate reports whether a plaintext password may be used. Only length is
// checked; composition rules are deliberately absent. See ADR 0010.
func Validate(plain string) error {
	// Runes, not bytes: four emoji are sixteen bytes but four characters.
	switch length := utf8.RuneCountInString(plain); {
	case length < MinLength:
		return ErrPasswordTooShort
	case length > MaxLength:
		return ErrPasswordTooLong
	default:
		return nil
	}
}
