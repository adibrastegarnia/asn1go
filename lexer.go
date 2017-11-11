package asn1go

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode"
	"unicode/utf8"
)

var (
	RESERVED_WORDS map[string]int
)

func init() {
	RESERVED_WORDS = map[string]int{
		"ABSENT":           ABSENT,
		"ENCODED":          ENCODED,
		"INTEGER":          INTEGER,
		"RELATIVE-OID":     RELATIVE_OID,
		"ABSTRACT-SYNTAX":  ABSTRACT_SYNTAX,
		"END":              END,
		"INTERSECTION":     INTERSECTION,
		"SEQUENCE":         SEQUENCE,
		"ALL":              ALL,
		"ENUMERATED":       ENUMERATED,
		"ISO646String":     ISO646String,
		"SET":              SET,
		"APPLICATION":      APPLICATION,
		"EXCEPT":           EXCEPT,
		"MAX":              MAX,
		"SIZE":             SIZE,
		"AUTOMATIC":        AUTOMATIC,
		"EXPLICIT":         EXPLICIT,
		"MIN":              MIN,
		"STRING":           STRING,
		"BEGIN":            BEGIN,
		"EXPORTS":          EXPORTS,
		"MINUS-INFINITY":   MINUS_INFINITY,
		"SYNTAX":           SYNTAX,
		"BIT":              BIT,
		"EXTENSIBILITY":    EXTENSIBILITY,
		"NULL":             NULL,
		"T61String":        T61String,
		"BMPString":        BMPString,
		"EXTERNAL":         EXTERNAL,
		"NumericString":    NumericString,
		"TAGS":             TAGS,
		"BOOLEAN":          BOOLEAN,
		"FALSE":            FALSE,
		"OBJECT":           OBJECT,
		"TeletexString":    TeletexString,
		"BY":               BY,
		"FROM":             FROM,
		"ObjectDescriptor": ObjectDescriptor,
		"TRUE":             TRUE,
		"CHARACTER":        CHARACTER,
		"GeneralizedTime":  GeneralizedTime,
		"OCTET":            OCTET,
		"TYPE-IDENTIFIER":  TYPE_IDENTIFIER,
		"CHOICE":           CHOICE,
		"GeneralString":    GeneralString,
		"OF":               OF,
		"UNION":            UNION,
		"CLASS":            CLASS,
		"GraphicString":    GraphicString,
		"OPTIONAL":         OPTIONAL,
		"UNIQUE":           UNIQUE,
		"COMPONENT":        COMPONENT,
		"IA5String":        IA5String,
		"PATTERN":          PATTERN,
		"UNIVERSAL":        UNIVERSAL,
		"COMPONENTS":       COMPONENTS,
		"IDENTIFIER":       IDENTIFIER,
		"PDV":              PDV,
		"UniversalString":  UniversalString,
		"CONSTRAINED":      CONSTRAINED,
		"IMPLICIT":         IMPLICIT,
		"PLUS-INFINITY":    PLUS_INFINITY,
		"UTCTime":          UTCTime,
		"CONTAINING":       CONTAINING,
		"IMPLIED":          IMPLIED,
		"PRESENT":          PRESENT,
		"UTF8String":       UTF8String,
		"DEFAULT":          DEFAULT,
		"IMPORTS":          IMPORTS,
		"PrintableString":  PrintableString,
		"VideotexString":   VideotexString,
		"DEFINITIONS":      DEFINITIONS,
		"INCLUDES":         INCLUDES,
		"PRIVATE":          PRIVATE,
		"VisibleString":    VisibleString,
		"EMBEDDED":         EMBEDDED,
		"INSTANCE":         INSTANCE,
		"REAL":             REAL,
		"WITH":             WITH,
	}
}

type MyLexer struct {
	bufReader *bufio.Reader
	err       error
	result    *ModuleDefinition

	runeStack []rune
}

func (lex *MyLexer) Lex(lval *yySymType) int {
	for {
		r, _, err := lex.readRune()
		if err == io.EOF {
			return 0
		}
		if err != nil {
			lex.Error(fmt.Sprintf("Failed to read: %v", err.Error()))
			return -1
		}

		// fast forward cases
		if isWhitespace(r) {
			continue
		} else if r == '-' && lex.peekRune() == '-' {
			lex.skipLineComment()
			continue
		} else if r == '/' && lex.peekRune() == '*' {
			lex.skipBlockComment()
			continue
		}

		// parse lexem
		if unicode.IsLetter(r) {
			lex.unreadRune()
			content, err := lex.consumeWord()
			if err != nil {
				lex.Error(err.Error())
				return -1
			}
			if unicode.IsUpper(r) {
				code, exists := RESERVED_WORDS[content]
				if exists {
					return code
				} else {
					lval.name = content
					return TYPEORMODULEREFERENCE
				}
			} else {
				lval.name = content
				return VALUEIDENTIFIER
			}
		} else if unicode.IsDigit(r) {
			lex.unreadRune()
			return lex.consumeNumberOrReal(lval, math.NaN())
		} else if r == '-' && unicode.IsDigit(lex.peekRune()) {
			return lex.consumeNumberOrReal(lval, -1)
		} else if r == ':' && lex.peekRunes(2) == ":=" {
			lex.discard(2)
			return ASSIGNMENT
		} else if r == '.' && lex.peekRunes(2) == ".." {
			lex.discard(2)
			return ELLIPSIS
		} else if r == '.' && lex.peekRune() == '.' {
			lex.discard(1)
			return RANGE_SEPARATOR
		} else if r == '[' && lex.peekRune() == '[' {
			lex.discard(1)
			return LEFT_VERSION_BRACKETS
		} else if r == ']' && lex.peekRune() == ']' {
			lex.discard(1)
			return RIGHT_VERSION_BRACKETS
		} else {
			return lex.consumeSingleSymbol(r)
		}
	}
}

func (lex *MyLexer) consumeNumberOrReal(lval *yySymType, realStart float64) int {
	// worknig on this function at 11 PM was bad idea
	realValue := realStart
	fullRepr := ""
	if realStart == -1 {
		fullRepr += "-"
	}
	res := lex.consumeNumber(lval)
	if res != NUMBER {
		return res
	}
	realValue *= float64(int(lval.number))
	fullRepr = lval.numberRepr
	if lex.peekRune() == '.' {
		if math.IsNaN(realValue) {
			realValue = float64(int(lval.number))
		}
		lex.readRune()
		fullRepr += "."
		if unicode.IsDigit(lex.peekRune()) {
			res := lex.consumeNumber(lval)
			if res != NUMBER {
				return res
			}
			fullRepr += lval.numberRepr
			shift := float64(math.Pow10(int(math.Ceil(math.Log10(float64(lval.number))))))
			realValue = realValue + float64(lval.number)/shift
		}
	}
	if unicode.ToLower(lex.peekRune()) == 'e' {
		if math.IsNaN(realValue) {
			realValue = float64(int(lval.number))
		}
		r, _, _ := lex.readRune()
		fullRepr += string(r)
		exponent := 1
		possibleSignRune := lex.peekRune()
		if possibleSignRune == '-' {
			exponent = -1
			fullRepr += string(possibleSignRune)
			lex.readRune()
		}
		firstExponentRune := lex.peekRune()
		if unicode.IsDigit(firstExponentRune) {
			res := lex.consumeNumber(lval)
			if res != NUMBER {
				return res
			}
			exponent *= int(lval.number)
			fullRepr += lval.numberRepr
			realValue *= math.Pow10(exponent)
		} else {
			lex.Error(fmt.Sprintf("Expected exponent after '%v' got '%c'", fullRepr, firstExponentRune))
		}
	}
	if math.IsNaN(realValue) {
		return NUMBER
	} else {
		lval.real = Real(realValue)
		lval.numberRepr = fullRepr
		return REALNUMBER
	}
}

func (lex *MyLexer) consumeSingleSymbol(r rune) int {
	switch r {
	case '{':
		return OPEN_CURLY
	case '}':
		return CLOSE_CURLY
	case '<':
		return LESS
	case '>':
		return GREATER
	case ',':
		return COMMA
	case '.':
		return DOT
	case '(':
		return OPEN_ROUND
	case ')':
		return CLOSE_ROUND
	case '[':
		return OPEN_SQUARE
	case ']':
		return CLOSE_SQUARE
	case '-':
		return MINUS
	case ':':
		return COLON
	case '=':
		return EQUALS
	case '"':
		return QUOTATION_MARK
	case '\'':
		return APOSTROPHE
	case ' ': // TODO at which context it can be parsed?
		return SPACE
	case ';':
		return SEMICOLON
	case '@':
		return AT
	case '|':
		return PIPE
	case '!':
		return EXCLAMATION
	case '^':
		return CARET
	default:
		lex.Error(fmt.Sprintf("Unexpected character: %c", r))
		return -1
	}
}

func (lex *MyLexer) unreadRune() error {
	r := lex.bufReader.UnreadRune()
	if r != nil {
		panic(r.Error())
	}
	return r
}

func (lex *MyLexer) readRune() (rune, int, error) {
	r, n, err := lex.bufReader.ReadRune()
	return r, n, err
}

func (lex *MyLexer) peekRune() rune {
	r, _ := lex.peekRuneE()
	return r
}

func (lex *MyLexer) discard(n int) {
	lex.bufReader.Discard(n)
}

func (lex *MyLexer) peekRunes(n int) string {
	acc := bytes.NewBufferString("")
	pos := 0
	for n > 0 {
		for l := 1; l <= utf8.UTFMax; l++ {
			buf, err := lex.bufReader.Peek(pos + l)
			slice := buf[pos : pos+l]
			if pos+l <= len(buf) && utf8.FullRune(slice) {
				r, size := utf8.DecodeRune(slice)
				acc.WriteRune(r)
				pos += size
				n -= 1
				break
			}
			if err == io.EOF { // TODO if it is not a full rune, will swallow the error
				return acc.String()
			}
		}
	}
	return acc.String()
}

func (lex *MyLexer) peekRuneE() (rune, error) {
	r, _, err := lex.bufReader.ReadRune()
	if err == nil {
		lex.bufReader.UnreadRune()
	}
	return r, err
}

func (lex *MyLexer) skipLineComment() {
	lastIsHyphen := false
	for {
		r, _, err := lex.readRune()
		if isNewline(r) || err == io.EOF {
			return
		} else if r == '-' {
			if lastIsHyphen {
				return
			}
			lastIsHyphen = true
		} else {
			lastIsHyphen = false
		}
	}
}

func (lex *MyLexer) skipBlockComment() {
	lastIsOpeningSlash := false
	lastIsClosingStar := false
	for {
		r, _, err := lex.readRune()
		if err == io.EOF {
			return
		}
		if r == '/' {
			if lastIsClosingStar {
				return
			} else {
				lastIsOpeningSlash = true
				continue
			}
		} else if r == '*' {
			if lastIsOpeningSlash {
				lex.skipBlockComment()
			} else {
				lastIsClosingStar = true
				continue
			}
		}
		lastIsClosingStar = false
		lastIsOpeningSlash = false
	}
}

func (lex *MyLexer) consumeWord() (string, error) {
	r, _, _ := lex.bufReader.ReadRune()
	acc := bytes.NewBufferString("")
	acc.WriteRune(r)
	lastR := r
	for {
		r, _, err := lex.readRune()
		if err == io.EOF || isWhitespace(r) || !isIdentifierChar(r) {
			label := acc.String()
			if label[len(label)-1] == '-' {
				return "", errors.New(fmt.Sprintf("Token can not end on hyphen, got %v", label))
			}
			if err == nil {
				lex.unreadRune()
			}
			return label, nil
		}
		if err != nil {
			return "", errors.New(fmt.Sprintf("Failed to read: %v", err.Error()))
		}
		if !isIdentifierChar(r) {
			acc.WriteRune(r)
			return "", errors.New(fmt.Sprintf("Expected valid identifier char, got '%c' while reading '%v'", r, acc.String()))
		}
		acc.WriteRune(r)
		if lastR == '-' && r == '-' {
			return "", errors.New(fmt.Sprintf("Token can not contain two hyphens in a row, got %v", acc.String()))
		}
		lastR = r
	}
}

func (lex *MyLexer) consumeNumber(lval *yySymType) int {
	r, _, err := lex.bufReader.ReadRune()
	if err != nil {
		lex.Error(err.Error())
		return -1
	}
	acc := bytes.NewBufferString("")
	acc.WriteRune(r)
	for {
		r, _, err := lex.readRune()
		if err == io.EOF || !unicode.IsDigit(r) {
			if err == nil && !unicode.IsDigit(r) {
				lex.unreadRune()
			}
			repr := acc.String()
			i, err := strconv.Atoi(repr)
			if err != nil {
				lex.Error(fmt.Sprintf("Failed to parse number: %v", err.Error()))
				return -1
			}
			lval.numberRepr = repr
			lval.number = Number(i)
			return NUMBER
		}
		if err != nil {
			lex.Error(fmt.Sprintf("Failed to read: %v", err.Error()))
			return -1
		}
		acc.WriteRune(r)
	}
}

func (lex *MyLexer) Error(e string) {
	lex.err = errors.New(e)
}

func isWhitespace(r rune) bool {
	switch x := int(r); x {
	//HORIZONTAL TABULATION (9)
	case 9:
		return true
	//LINE FEED (10)
	case 10:
		return true
	//VERTICAL TABULATION (11)
	case 11:
		return true
	//FORM FEED (12)
	case 12:
		return true
	//CARRIAGE RETURN (13)
	case 13:
		return true
	//SPACE (32)
	case 32:
		return true
	default:
		return false
	}
}

func isNewline(r rune) bool {
	switch x := int(r); x {
	//LINE FEED (10)
	case 10:
		return true
	//VERTICAL TABULATION (11)
	case 11:
		return true
	//FORM FEED (12)
	case 12:
		return true
	//CARRIAGE RETURN (13)
	case 13:
		return true
	default:
		return false
	}
}

func isIdentifierChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-'
}
