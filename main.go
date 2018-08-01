package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type item struct {
	typ    itemType // Type, such as itemNumber
	val    string   // Value, such as "6523.423e23"
	offset int      // token position in string
}

type itemType int

const (
	itemError itemType = iota // error occurred

	itemEOF
	itemString
	itemNumber
	itemObjectBegin
	itemObjectEnd
	itemBool
	itemArrayBegin
	itemArrayEnd
	itemColon
	itemSeparator
	itemNull
)

const (
	eof             = 0
	openObject      = '{'
	closeObject     = '}'
	openArray       = '['
	closeArray      = ']'
	objectSeparator = ':'
	stringStart     = '"'
	stringEnd       = '"'
	separator       = ','
	tre             = "true"
	fls             = "false"
	nll             = "null"
)

type stateFn func(lx *lexer) stateFn

type lexer struct {
	name  string // used only for error reports
	input string // the string being scanned
	start int    // start position of this item
	pos   int    // current position in the input
	line  int
	width int // width of last rune read from input
	state stateFn
	items chan item
}

func (i item) String() string {
	switch i.typ {
	case itemEOF:
		return "EOF"
	case itemError:
		return i.val
	}
	// if len(i.val) > 10 {
	// 	return fmt.Sprintf("%.10q... at position: %d of type: %d", i.val, i.offset, i.typ)
	// }
	// return fmt.Sprintf("%q at position: %d of type: %d", i.val, i.offset, i.typ)
	if len(i.val) > 10 {
		return fmt.Sprintf("%.10q...", i.val)
	}
	return fmt.Sprintf("%q", i.val)
}

func lex(name, input string) (*lexer, chan item) {
	lx := &lexer{
		name:  name,
		input: input,
		items: make(chan item, 2),
	}
	//go lx.run()
	return lx, lx.items
}

func (lx *lexer) emit(t itemType) {
	lx.items <- item{t, lx.input[lx.start:lx.pos], lx.start}
	lx.start = lx.pos
}

func (lx *lexer) ignore() {
	lx.start = lx.pos
}

func (lx *lexer) backup() {
	lx.pos -= lx.width
}

func (lx *lexer) peek() rune {
	rne := lx.next()
	lx.backup()
	return rne
}

func (lx *lexer) accept(valid string) bool {
	if strings.ContainsRune(valid, lx.next()) {
		return true
	}
	lx.backup()
	return false
}

func (lx *lexer) acceptRun(valid string) {
	for strings.ContainsRune(valid, lx.next()) {
	}
	lx.backup()
}

func (lx *lexer) next() rune {
	var rne rune
	if lx.pos >= len(lx.input) {
		lx.width = 0
		return eof
	}
	rne, lx.width = utf8.DecodeRuneInString(lx.input[lx.pos:])
	lx.pos += lx.width
	return rne
}

func (lx *lexer) errorf(format string, args ...interface{}) stateFn {
	lx.items <- item{
		itemError,
		fmt.Sprintf(format, args...),
		lx.start,
	}
	return nil
}

func lexNumber(lx *lexer) stateFn {
	// option leading sign
	lx.accept("+-")
	// is it hex?
	digits := "0123456789"
	if lx.accept("0") && lx.accept("xX") {
		digits = "0123456789abcdefABCDEF"
	}
	lx.acceptRun(digits)
	if lx.accept(".") {
		lx.acceptRun(digits)
	}
	if lx.accept("eE") {
		lx.accept("+-")
		lx.acceptRun("0123456789")
	}
	// is it imaginary?
	lx.accept("i")
	// next value must not be alphanumeric
	pk := lx.peek()
	if unicode.IsDigit(pk) || unicode.IsLetter(pk) {
		lx.next()
		return lx.errorf("bad number syntax: %q", lx.input[lx.start:lx.pos])
	}
	lx.emit(itemNumber)
	return lexAfterValueHelper(lx)
}

func lexSeparator(lx *lexer) stateFn {
	//	lx.pos++
	lx.emit(itemSeparator)

	for {
		switch r := lx.next(); {
		case r == eof:
			return lx.errorf("unexpected EOF")
		case r == stringStart:
			return lexString
		case r == openObject:
			return lexOpenObject
		case r == openArray:
			return lexOpenArray
		case unicode.IsDigit(r):
			return lexNumber
		case unicode.IsSpace(r):
			lx.ignore()
		case strings.HasPrefix(lx.input[lx.pos-1:], tre):
			return lexTrue
		case strings.HasPrefix(lx.input[lx.pos-1:], fls):
			return lexFalse
		case strings.HasPrefix(lx.input[lx.pos-1:], nll):
			return lexNull
		default:
			lx.errorf("unexpected symbol %s", r)
		}
	}
}

func lexBegin(lx *lexer) stateFn {
	switch r := lx.next(); {
	case r == openObject:
		return lexOpenObject
	default:
		return lx.errorf("json must start with '{'}")
	}
}

func lexOpenObject(lx *lexer) stateFn {
	// fmt.Println("lexOpenObject")
	lx.emit(itemObjectBegin)

	for {
		switch r := lx.next(); {
		case r == eof:
			return lx.errorf("unclosed action") // TODO make better message
		case r == stringStart:
			return lexString
		case r == closeObject:
			return lexCloseObject
		case unicode.IsSpace(r):
			lx.ignore()
		default:
			return lx.errorf("expected string or '}'")
		}
	}
}

func lexCloseObject(lx *lexer) stateFn {
	lx.emit(itemObjectEnd)

	for {
		switch r := lx.next(); {
		case r == eof:
			lx.emit(itemEOF)
			return nil // it is ok to end on closing bracket
		case r == separator:
			return lexSeparator
		case r == closeObject:
			return lexCloseObject
		case r == closeArray:
			return lexCloseArray
		case unicode.IsSpace(r):
			lx.ignore()
		default:
			return lx.errorf("invalid character: %s", string(r))
		}
	}
}

func lexOpenArray(lx *lexer) stateFn {
	lx.emit(itemArrayBegin)
	for {
		switch r := lx.next(); {
		case r == eof:
			return lx.errorf("unexpected eof")
		case r == openArray:
			return lexOpenArray
		case r == openObject:
			return lexOpenObject
		case r == closeArray:
			return lexCloseArray
		case r == stringStart:
			return lexString
		case unicode.IsDigit(r):
			return lexNumber
		case unicode.IsSpace(r):
			fmt.Println("ignored whitespace")
			lx.ignore()
		case strings.HasPrefix(lx.input[lx.pos-1:], tre):
			return lexTrue
		case strings.HasPrefix(lx.input[lx.pos-1:], fls):
			return lexFalse
		case strings.HasPrefix(lx.input[lx.pos-1:], nll):
			return lexNull
		default:
			return lx.errorf("unexpected symbol: %s", string(r))
		}
	}
}

func lexNull(lx *lexer) stateFn {
	lx.pos += len(nll) - 1
	lx.emit(itemNull)

	return lexAfterValueHelper(lx)
}

func lexTrue(lx *lexer) stateFn {
	lx.pos += len(tre) - 1
	lx.emit(itemBool)

	return lexAfterValueHelper(lx)
}

func lexFalse(lx *lexer) stateFn {
	lx.pos += len(fls) - 1
	lx.emit(itemBool)

	return lexAfterValueHelper(lx)
}

func lexAfterValueHelper(lx *lexer) stateFn {
	for {
		switch r := lx.next(); {
		case r == eof:
			return lx.errorf("unexpected eof")
		case r == separator:
			return lexSeparator
		case r == closeArray:
			return lexCloseArray
		case r == closeObject:
			return lexCloseObject
		case unicode.IsSpace(r):
			lx.ignore()
		default:
			return lx.errorf("unexpected symbol: %s", string(r))
		}
	}
}

func lexCloseArray(lx *lexer) stateFn {
	lx.emit(itemArrayEnd)
	for {
		switch r := lx.next(); {
		case r == eof:
			return lx.errorf("unexpected eof")
		case r == closeArray:
			return lexCloseArray
		case r == closeObject:
			return lexCloseObject
		case r == separator:
			return lexSeparator
		case unicode.IsSpace(r):
			lx.ignore()
		default:
			return lx.errorf("unexpected symbol: %s", string(r))
		}
	}
}

func lexString(lx *lexer) stateFn {
	for {
		switch r := lx.next(); {
		case r == eof:
			return lx.errorf("unexpected EOF")
		case r == '\\':
			// do a bunc of crap
			// TODO
		case r == '\n' || r == '\r':
			return lx.errorf("strings cannot contain newlines")
		case r == stringEnd:
			lx.emit(itemString)
			for {
				switch r := lx.next(); {
				case r == eof:
					return lx.errorf("unexpected EOF")
				case r == objectSeparator:
					return lexColon
				case r == separator:
					return lexSeparator
				case r == closeArray:
					return lexCloseArray
				case r == closeObject:
					return lexCloseObject
				case unicode.IsSpace(r):
					lx.ignore()
				default:
					return lx.errorf("unexpected symbol: %s", string(r))
				}
			}
		}
	}
}

func lexColon(lx *lexer) stateFn {
	lx.emit(itemColon)

	for {
		switch r := lx.next(); {
		case r == eof:
			return lx.errorf("unexpected eof")
		case r == openArray:
			return lexOpenArray
		case r == openObject:
			return lexOpenObject
		case r == stringStart:
			return lexString
		case unicode.IsSpace(r):
			lx.ignore()
		case strings.HasPrefix(lx.input[lx.pos-1:], tre):
			return lexTrue
		case strings.HasPrefix(lx.input[lx.pos-1:], fls):
			return lexFalse
		case strings.HasPrefix(lx.input[lx.pos-1:], nll):
			return lexNull
		default:
			return lx.errorf("unexpected symbol: %s", string(r))
		}
	}
}

func lexText(lx *lexer) stateFn {
	for {
		if strings.HasPrefix(lx.input[lx.pos:], string(openObject)) {
			// fmt.Println("lexText: has openObject")
			return lexOpenObject // next state
		}
		if lx.next() == eof {
			break
		}
	}
	// correctly reached EOF
	// if lx.pos > lx.start {
	// 	lx.emit(itemText)
	// }
	lx.emit(itemEOF)
	return nil
}

func (lx *lexer) nextItem() item {
	// fmt.Println("nextItem")
	for {
		select {
		case item := <-lx.items:
			// fmt.Println("nexItem: returning item")
			return item
		default:
			// fmt.Println("nexItem: default")
			lx.state = lx.state(lx)
		}
	}
	panic("not reached")
}

func main() {
	someJSON := `{"this": "that", "hello":[[3], 3  , "hello", null], "yoyoyo": {"itis":  true }, "crap": null, "yes": true }`

	lxer, _ := lex("test", someJSON)
	lxer.state = lexBegin

	items := make([]item, 0)

	for item := lxer.nextItem(); item.typ != itemEOF; item = lxer.nextItem() {
		items = append(items, item)
	}

	fmt.Println(items)
}

// func (lx *lexer) run() {
// 	fmt.Println("run")
// 	for state := lexText; state != nil; {
// 		fmt.Println("run inside")
// 		state = state(lx)
// 	}
// 	close(lx.items)
// }
