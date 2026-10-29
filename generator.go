package main

// generator.go emits the deterministic single-tape sorter in machine.tsv.
//
// Each TSV row contains:
//
//   current state, read symbol, move, write symbol, next state
//
// Go only constructs the transition table; the generated Turing machine does
// the sorting. Use `go run generator.go --check` to verify the checked-in TSV.

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"unicode/utf8"
)

const (
	hexDigits    = "0123456789ABCDEF"
	markerDigits = "abcdefghijklmnop"
	wordLength   = 16
	initialState = "⎆"
	acceptState  = "✔"
	blank        = '·'
	stop         = "⏹"
	hole         = '*'
)

var (
	hexRunes    = []rune(hexDigits)
	markerRunes = []rune(markerDigits)
	hexRank     = make(map[rune]int, wordLength)
	mark        = make(map[rune]rune, wordLength)
	unmark      = make(map[rune]rune, wordLength)
	moves       = map[string]string{"L": "←", "R": "→", "S": stop}
)

type rule struct {
	state     string
	read      rune
	move      string
	write     rune
	nextState string
}

type transitionKey struct {
	state string
	read  rune
}

type builder struct {
	rules []rule
	seen  map[transitionKey]rule
}

func init() {
	for i, digit := range hexRunes {
		hexRank[digit] = i
		mark[digit] = markerRunes[i]
		unmark[markerRunes[i]] = digit
	}
	if err := validateAlphabet(); err != nil {
		panic(err)
	}
}

func validateAlphabet() error {
	if len(hexRunes) != wordLength || len(markerRunes) != wordLength {
		return errors.New("hex and marker alphabets must each contain 16 symbols")
	}
	groups := [][]rune{
		hexRunes,
		markerRunes,
		{hole},
		{'[', ']', ',', blank},
	}
	for i, group := range groups {
		seen := make(map[rune]bool)
		for _, symbol := range group {
			if seen[symbol] {
				return fmt.Errorf("duplicate symbol %q within alphabet group", symbol)
			}
			seen[symbol] = true
		}
		for _, other := range groups[i+1:] {
			for _, symbol := range other {
				if seen[symbol] {
					return fmt.Errorf("tape alphabet groups overlap on %q", symbol)
				}
			}
		}
	}
	return nil
}

func newBuilder() *builder {
	return &builder{seen: make(map[transitionKey]rule)}
}

func (b *builder) add(state string, read rune, direction string, write rune, nextState string) {
	if state == "" || nextState == "" {
		panic("state names cannot be empty")
	}
	if utf8.RuneCountInString(string(read)) != 1 || utf8.RuneCountInString(string(write)) != 1 {
		panic("read and write symbols must each occupy one tape cell")
	}
	move, ok := moves[direction]
	if !ok {
		panic(fmt.Sprintf("unknown move %q", direction))
	}
	if state == acceptState {
		panic("the accepting state cannot have outgoing transitions")
	}
	if (direction == "S") != (nextState == acceptState) {
		panic("exactly transitions into ✔ must use the halt move")
	}

	key := transitionKey{state: state, read: read}
	entry := rule{state: state, read: read, move: move, write: write, nextState: nextState}
	if previous, exists := b.seen[key]; exists {
		panic(fmt.Sprintf("duplicate transition for %v: %v / %v", key, previous, entry))
	}
	b.seen[key] = entry
	b.rules = append(b.rules, entry)
}

func (r rule) line() string {
	return fmt.Sprintf("%s\t%c\t%s\t%c\t%s", r.state, r.read, r.move, r.write, r.nextState)
}

func (b *builder) write(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	stream := bufio.NewWriter(file)
	for _, entry := range b.rules {
		if _, err := fmt.Fprintln(stream, entry.line()); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := stream.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (b *builder) checkFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for index, expected := range b.rules {
		lineNumber := index + 1
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return fmt.Errorf("%s is stale at line %d: found end of file, expected %q", path, lineNumber, expected.line())
		}
		if actual := scanner.Text(); actual != expected.line() {
			return fmt.Errorf("%s is stale at line %d: found %q, expected %q", path, lineNumber, actual, expected.line())
		}
	}
	if scanner.Scan() {
		return fmt.Errorf("%s has unexpected extra content at line %d", path, len(b.rules)+1)
	}
	return scanner.Err()
}

func buildMachine() *builder {
	// Bubble-sort control flow:
	//
	//   repeat
	//       swapped = false
	//       for each adjacent pair (left, right)
	//           if left > right: swap and set swapped = true
	//   until not swapped
	//
	// State suffix _0/_1 stores the pass flag. Marked prefixes identify the
	// current digit position, so no position counters are needed in state names.
	b := newBuilder()
	b.add(initialState, '[', "R", '[', "VALIDATE")

	// Validate the complete input alphabet before entering any state that
	// interprets a-p as internal comparison markers. Without this pass, an
	// invalid marker at the start of the second word can be mistaken for an
	// already-compared digit and later rewritten as valid hexadecimal input.
	for _, symbol := range []rune(hexDigits + ",") {
		b.add("VALIDATE", symbol, "R", symbol, "VALIDATE")
	}
	b.add("VALIDATE", ']', "L", ']', "VALIDATE_BACK")
	for _, symbol := range []rune(hexDigits + ",") {
		b.add("VALIDATE_BACK", symbol, "L", symbol, "VALIDATE_BACK")
	}
	b.add("VALIDATE_BACK", '[', "R", '[', "CHECK_0")

	// FIND ADJACENT PAIRS. CHECK scans the current word. BACK returns from its
	// following comma to the word's first digit.
	for flagValue := 0; flagValue <= 1; flagValue++ {
		check := fmt.Sprintf("CHECK_%d", flagValue)
		for _, digit := range hexRunes {
			b.add(check, digit, "R", digit, check)
		}
		b.add(check, ',', "L", ',', fmt.Sprintf("BACK_%d", flagValue))
		b.add(check, ']', "L", ']', fmt.Sprintf("END_%d", flagValue))

		back := fmt.Sprintf("BACK_%d", flagValue)
		for _, digit := range hexRunes {
			b.add(back, digit, "L", digit, back)
		}
		b.add(back, '[', "R", '[', fmt.Sprintf("CMP_L_%d", flagValue))
		b.add(back, ',', "R", ',', fmt.Sprintf("CMP_L_%d", flagValue))
	}

	// COMPARE. The state remembers one left digit. The marked right prefix has
	// the same length as the marked left prefix, so the first unmarked right
	// digit is always the corresponding position.
	for flagValue := 0; flagValue <= 1; flagValue++ {
		compareLeft := fmt.Sprintf("CMP_L_%d", flagValue)
		for _, leftDigit := range hexRunes {
			seek := fmt.Sprintf("CMP_SEEK_%d_%c", flagValue, leftDigit)
			compareRight := fmt.Sprintf("CMP_R_%d_%c", flagValue, leftDigit)
			b.add(compareLeft, leftDigit, "R", mark[leftDigit], seek)
			for _, digit := range hexRunes {
				b.add(seek, digit, "R", digit, seek)
			}
			b.add(seek, ',', "R", ',', compareRight)

			for _, marker := range markerRunes {
				b.add(compareRight, marker, "R", marker, compareRight)
			}
			for _, rightDigit := range hexRunes {
				if leftDigit == rightDigit {
					b.add(compareRight, rightDigit, "L", mark[rightDigit], fmt.Sprintf("EQ_RBACK_%d", flagValue))
					continue
				}
				action := "KEEP"
				if hexRank[leftDigit] > hexRank[rightDigit] {
					action = "SWAP"
				}
				b.add(compareRight, rightDigit, "L", rightDigit, fmt.Sprintf("RESTORE_R_%d_%s", flagValue, action))
			}
		}

		// Returning to CMP_L on the comma means all sixteen positions matched.
		b.add(compareLeft, ',', "R", ',', fmt.Sprintf("EQUAL_TO_END_%d", flagValue))

		equalRightBack := fmt.Sprintf("EQ_RBACK_%d", flagValue)
		for _, marker := range markerRunes {
			b.add(equalRightBack, marker, "L", marker, equalRightBack)
		}
		b.add(equalRightBack, ',', "L", ',', fmt.Sprintf("EQ_LBACK_%d", flagValue))

		equalLeftBack := fmt.Sprintf("EQ_LBACK_%d", flagValue)
		for _, digit := range hexRunes {
			b.add(equalLeftBack, digit, "L", digit, equalLeftBack)
		}
		for _, marker := range markerRunes {
			b.add(equalLeftBack, marker, "R", marker, compareLeft)
		}

		equalToEnd := fmt.Sprintf("EQUAL_TO_END_%d", flagValue)
		for _, marker := range markerRunes {
			b.add(equalToEnd, marker, "R", marker, equalToEnd)
		}
		b.add(equalToEnd, ',', "L", ',', fmt.Sprintf("RESTORE_R_%d_KEEP", flagValue))
		b.add(equalToEnd, ']', "L", ']', fmt.Sprintf("RESTORE_R_%d_KEEP", flagValue))

		// Restore all comparison markers before either action.
		for _, action := range []string{"KEEP", "SWAP"} {
			restoreRight := fmt.Sprintf("RESTORE_R_%d_%s", flagValue, action)
			for _, marker := range markerRunes {
				b.add(restoreRight, marker, "L", unmark[marker], restoreRight)
			}
			restoreLeft := fmt.Sprintf("RESTORE_L_%d_%s", flagValue, action)
			b.add(restoreRight, ',', "L", ',', restoreLeft)
			for _, digit := range hexRunes {
				b.add(restoreLeft, digit, "L", digit, restoreLeft)
			}
			for _, marker := range markerRunes {
				b.add(restoreLeft, marker, "L", unmark[marker], restoreLeft)
			}
			target := "SWAP_L"
			if action == "KEEP" {
				target = fmt.Sprintf("ADVANCE_%d", flagValue)
			}
			b.add(restoreLeft, '[', "R", '[', target)
			b.add(restoreLeft, ',', "R", ',', target)
		}

		advance := fmt.Sprintf("ADVANCE_%d", flagValue)
		for _, digit := range hexRunes {
			b.add(advance, digit, "R", digit, advance)
		}
		b.add(advance, ',', "R", ',', fmt.Sprintf("CHECK_%d", flagValue))
	}

	// SWAP. '*' is a one-cell hole. Finite control carries the removed left
	// digit rightward and the displaced right digit back to the hole.
	for _, leftDigit := range hexRunes {
		seek := fmt.Sprintf("SWAP_SEEK_%c", leftDigit)
		swapRight := fmt.Sprintf("SWAP_R_%c", leftDigit)
		b.add("SWAP_L", leftDigit, "R", hole, seek)
		for _, digit := range hexRunes {
			b.add(seek, digit, "R", digit, seek)
		}
		b.add(seek, ',', "R", ',', swapRight)
		for _, marker := range markerRunes {
			b.add(swapRight, marker, "R", marker, swapRight)
		}
		for _, rightDigit := range hexRunes {
			b.add(swapRight, rightDigit, "L", mark[leftDigit], fmt.Sprintf("SWAP_BACK_R_%c", rightDigit))
		}
	}

	for _, rightDigit := range hexRunes {
		backRight := fmt.Sprintf("SWAP_BACK_R_%c", rightDigit)
		for _, marker := range markerRunes {
			b.add(backRight, marker, "L", marker, backRight)
		}
		backLeft := fmt.Sprintf("SWAP_BACK_L_%c", rightDigit)
		b.add(backRight, ',', "L", ',', backLeft)
		for _, digit := range hexRunes {
			b.add(backLeft, digit, "L", digit, backLeft)
		}
		b.add(backLeft, hole, "R", mark[rightDigit], "SWAP_L")
	}

	b.add("SWAP_L", ',', "L", ',', "SWAP_RESTORE_L")
	for _, marker := range markerRunes {
		b.add("SWAP_RESTORE_L", marker, "L", unmark[marker], "SWAP_RESTORE_L")
	}
	b.add("SWAP_RESTORE_L", '[', "R", '[', "SWAP_TO_COMMA")
	b.add("SWAP_RESTORE_L", ',', "R", ',', "SWAP_TO_COMMA")
	for _, digit := range hexRunes {
		b.add("SWAP_TO_COMMA", digit, "R", digit, "SWAP_TO_COMMA")
	}
	b.add("SWAP_TO_COMMA", ',', "R", ',', "SWAP_RESTORE_R")
	for _, marker := range markerRunes {
		b.add("SWAP_RESTORE_R", marker, "R", unmark[marker], "SWAP_RESTORE_R")
	}
	b.add("SWAP_RESTORE_R", ',', "L", ',', "SWAP_BACK_INTERNAL")
	b.add("SWAP_RESTORE_R", ']', "L", ']', "SWAP_BACK_INTERNAL")
	for _, digit := range hexRunes {
		b.add("SWAP_BACK_INTERNAL", digit, "L", digit, "SWAP_BACK_INTERNAL")
	}
	b.add("SWAP_BACK_INTERNAL", ',', "R", ',', "CHECK_1")

	// END_0 accepts at cell 0. END_1 returns to cell 0 and starts a new pass.
	for flagValue := 0; flagValue <= 1; flagValue++ {
		end := fmt.Sprintf("END_%d", flagValue)
		for _, symbol := range []rune(hexDigits + ",") {
			b.add(end, symbol, "L", symbol, end)
		}
	}
	b.add("END_0", '[', "S", '[', acceptState)
	b.add("END_1", '[', "R", '[', "CHECK_0")

	return b
}

func main() {
	output := flag.String("output", "machine.tsv", "TSV destination")
	check := flag.Bool("check", false, "verify the destination without rewriting it")
	flag.Parse()

	machine := buildMachine()
	if *check {
		if err := machine.checkFile(*output); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%s is current (%d transitions)\n", *output, len(machine.rules))
		return
	}
	if err := machine.write(*output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d transitions to %s\n", len(machine.rules), *output)
}
