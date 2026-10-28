package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

type parsedTransition struct {
	write     rune
	move      string
	nextState string
}

type runResult struct {
	tape          map[int]rune
	state         string
	head          int
	steps         int64
	changedWrites int64
}

func loadMachine(t *testing.T) map[transitionKey]parsedTransition {
	t.Helper()
	file, err := os.Open("machine.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	transitions := make(map[transitionKey]parsedTransition)
	defined := make(map[string]bool)
	targets := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 5 {
			t.Fatalf("line %d: expected 5 tab-separated fields, got %d", lineNumber, len(fields))
		}
		state, readText, move, writeText, nextState := fields[0], fields[1], fields[2], fields[3], fields[4]
		if state == "" || nextState == "" {
			t.Fatalf("line %d: state names cannot be empty", lineNumber)
		}
		if utf8.RuneCountInString(readText) != 1 || utf8.RuneCountInString(writeText) != 1 {
			t.Fatalf("line %d: read/write symbols must each occupy one cell", lineNumber)
		}
		if move != "→" && move != "←" && move != stop {
			t.Fatalf("line %d: illegal move %q", lineNumber, move)
		}
		if state == acceptState {
			t.Fatalf("line %d: accepting state has an outgoing transition", lineNumber)
		}
		if (move == stop) != (nextState == acceptState) {
			t.Fatalf("line %d: exactly transitions into %s must halt", lineNumber, acceptState)
		}
		read := []rune(readText)[0]
		write := []rune(writeText)[0]
		key := transitionKey{state: state, read: read}
		if _, exists := transitions[key]; exists {
			t.Fatalf("line %d: duplicate transition for state %q and symbol %q", lineNumber, state, read)
		}
		transitions[key] = parsedTransition{write: write, move: move, nextState: nextState}
		defined[state] = true
		targets[nextState] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lineNumber == 0 {
		t.Fatal("machine.tsv is empty")
	}
	if !defined[initialState] {
		t.Fatalf("initial state %q is not defined", initialState)
	}
	if !targets[acceptState] {
		t.Fatalf("accepting state %q is never entered", acceptState)
	}
	for target := range targets {
		if target != acceptState && !defined[target] {
			t.Fatalf("transition targets undefined state %q", target)
		}
	}
	return transitions
}

func encode(words []string) string {
	return "[" + strings.Join(words, ",") + "]"
}

func inputWordCount(input string) int {
	if input == "[]" {
		return 0
	}
	return (utf8.RuneCountInString(input) - 1) / (wordLength + 1)
}

func runMachine(t *testing.T, transitions map[transitionKey]parsedTransition, input string) runResult {
	t.Helper()
	tape := make(map[int]rune)
	for index, symbol := range []rune(input) {
		tape[index] = symbol
	}

	words := inputWordCount(input)
	limit := int64(2_000) * int64(words+1) * int64(words+1)
	if limit < 10_000 {
		limit = 10_000
	}

	result := runResult{tape: tape, state: initialState}
	for result.state != acceptState {
		symbol, exists := result.tape[result.head]
		if !exists {
			symbol = blank
		}
		transition, exists := transitions[transitionKey{state: result.state, read: symbol}]
		if !exists {
			t.Fatalf("stuck after %d steps in state %q on %q at cell %d", result.steps, result.state, symbol, result.head)
		}
		if transition.write != symbol {
			result.changedWrites++
		}
		result.tape[result.head] = transition.write
		result.state = transition.nextState
		switch transition.move {
		case "→":
			result.head++
		case "←":
			result.head--
		case stop:
		default:
			t.Fatalf("unexpected move %q", transition.move)
		}
		result.steps++
		if result.steps > limit {
			t.Fatalf("step limit %d exceeded for %s", limit, input)
		}
	}
	return result
}

func assertSorts(t *testing.T, transitions map[transitionKey]parsedTransition, words []string) {
	t.Helper()
	supplied := append([]string(nil), words...)
	expectedWords := append([]string(nil), supplied...)
	sort.Strings(expectedWords)
	expected := []rune(encode(expectedWords))
	result := runMachine(t, transitions, encode(supplied))

	if result.state != acceptState {
		t.Fatalf("input %v: final state %q, want %q", supplied, result.state, acceptState)
	}
	if result.head != 0 {
		t.Fatalf("input %v: final head %d, want 0", supplied, result.head)
	}

	nonblank := 0
	for index, symbol := range result.tape {
		if symbol == blank {
			continue
		}
		nonblank++
		if index < 0 || index >= len(expected) {
			t.Fatalf("input %v: unexpected nonblank %q at cell %d", supplied, symbol, index)
		}
		if symbol != expected[index] {
			t.Fatalf("input %v: cell %d is %q, want %q", supplied, index, symbol, expected[index])
		}
		if !strings.ContainsRune("[],"+hexDigits, symbol) {
			t.Fatalf("input %v: scratch symbol %q remains at cell %d", supplied, symbol, index)
		}
	}
	if nonblank != len(expected) {
		t.Fatalf("input %v: found %d nonblank cells, want %d", supplied, nonblank, len(expected))
	}
	for index, symbol := range expected {
		if result.tape[index] != symbol {
			t.Fatalf("input %v: missing expected %q at cell %d", supplied, symbol, index)
		}
	}

	n := len(supplied)
	writeBound := int64(104) * int64(n) * int64(n-1)
	if result.changedWrites > writeBound {
		t.Fatalf("input %v: %d changed writes exceeds bound %d", supplied, result.changedWrites, writeBound)
	}
}

func TestGeneratedMachineCurrent(t *testing.T) {
	if err := buildMachine().checkFile("machine.tsv"); err != nil {
		t.Fatal(err)
	}
}

func TestCompactTableSize(t *testing.T) {
	transitions := loadMachine(t)
	states := map[string]bool{acceptState: true}
	for key, transition := range transitions {
		states[key.state] = true
		states[transition.nextState] = true
	}
	if len(transitions) != 3_498 {
		t.Fatalf("got %d transitions, want 3498", len(transitions))
	}
	if len(states) != 159 {
		t.Fatalf("got %d states, want 159", len(states))
	}
}

func TestEveryStateReachable(t *testing.T) {
	transitions := loadMachine(t)
	graph := make(map[string]map[string]bool)
	allStates := map[string]bool{acceptState: true}
	for key, transition := range transitions {
		if graph[key.state] == nil {
			graph[key.state] = make(map[string]bool)
		}
		graph[key.state][transition.nextState] = true
		allStates[key.state] = true
		allStates[transition.nextState] = true
	}
	reachable := map[string]bool{initialState: true}
	work := []string{initialState}
	for len(work) > 0 {
		state := work[len(work)-1]
		work = work[:len(work)-1]
		for nextState := range graph[state] {
			if !reachable[nextState] {
				reachable[nextState] = true
				work = append(work, nextState)
			}
		}
	}
	if len(reachable) != len(allStates) {
		t.Fatalf("%d of %d states are graph-reachable", len(reachable), len(allStates))
	}
}

func TestScratchAlphabetDisjoint(t *testing.T) {
	groups := []string{hexDigits, markerDigits, string(hole), "[]," + string(blank)}
	for i, group := range groups {
		seen := make(map[rune]bool)
		for _, symbol := range group {
			if seen[symbol] {
				t.Fatalf("duplicate symbol %q within alphabet group", symbol)
			}
			seen[symbol] = true
		}
		for _, other := range groups[i+1:] {
			for _, symbol := range other {
				if seen[symbol] {
					t.Fatalf("alphabet groups overlap on %q", symbol)
				}
			}
		}
	}
}

func TestRequiredEdgeCases(t *testing.T) {
	transitions := loadMachine(t)
	zero := strings.Repeat("0", wordLength)
	maximum := strings.Repeat("F", wordLength)
	cases := [][]string{
		{},
		{zero},
		{zero, maximum},
		{maximum, zero},
		{"1234567890ABCDEF", "1234567890ABCDEF"},
		{maximum, maximum, maximum},
		{maximum, "E000000000000000", "A000000000000000", zero},
		{"F000000000000000", "0000000000000001"},
		{"0000000000000001", zero},
		{"AAAAAAAAAAAAAAAB", "AAAAAAAAAAAAAAAA"},
		{"0000000000000002", "0000000000000001", zero},
	}
	for _, words := range cases {
		assertSorts(t, transitions, words)
	}
}

func TestEveryDigitOrderAtEveryPosition(t *testing.T) {
	transitions := loadMachine(t)
	for position := 0; position < wordLength; position++ {
		for _, leftDigit := range hexRunes {
			for _, rightDigit := range hexRunes {
				left := []rune(strings.Repeat("0", wordLength))
				right := []rune(strings.Repeat("0", wordLength))
				left[position] = leftDigit
				right[position] = rightDigit
				assertSorts(t, transitions, []string{string(left), string(right)})
			}
		}
	}
}

func randomWord(rng *rand.Rand, digits string) string {
	word := make([]byte, wordLength)
	for i := range word {
		word[i] = digits[rng.Intn(len(digits))]
	}
	return string(word)
}

// Random lists of 0-12 words, checked against Go's own sort.
func TestRandomLists(t *testing.T) {
	transitions := loadMachine(t)
	rng := rand.New(rand.NewSource(41080))
	for trial := 0; trial < 150; trial++ {
		n := rng.Intn(13)
		words := make([]string, n)
		for i := range words {
			words[i] = randomWord(rng, hexDigits)
		}
		t.Run(fmt.Sprintf("trial_%d_n_%d", trial, n), func(t *testing.T) {
			assertSorts(t, transitions, words)
		})
	}
}

// Random lists where words share long prefixes, so comparisons go deep.
func TestRandomSharedPrefixes(t *testing.T) {
	transitions := loadMachine(t)
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 60; trial++ {
		prefix := randomWord(rng, hexDigits)[:rng.Intn(wordLength)]
		n := rng.Intn(10)
		words := make([]string, n)
		for i := range words {
			words[i] = prefix + randomWord(rng, "0F8")[len(prefix):]
		}
		t.Run(fmt.Sprintf("trial_%d_prefix_%d", trial, len(prefix)), func(t *testing.T) {
			assertSorts(t, transitions, words)
		})
	}
}

// Many duplicates: only two distinct values in a longer list.
func TestManyDuplicates(t *testing.T) {
	transitions := loadMachine(t)
	low := strings.Repeat("7", wordLength)
	high := strings.Repeat("7", wordLength-1) + "8"
	words := []string{high, low, high, high, low, low, high, low, high, low}
	assertSorts(t, transitions, words)
}

// Longer lists: all 16 single-digit words in reverse, and 30 random words.
func TestLongLists(t *testing.T) {
	transitions := loadMachine(t)
	var reversed []string
	for i := len(hexRunes) - 1; i >= 0; i-- {
		reversed = append(reversed, strings.Repeat(string(hexRunes[i]), wordLength))
	}
	assertSorts(t, transitions, reversed)

	rng := rand.New(rand.NewSource(30))
	words := make([]string, 30)
	for i := range words {
		words[i] = randomWord(rng, hexDigits)
	}
	assertSorts(t, transitions, words)
}

// Worst case for writes: reverse sorted, words differ only in the last digit,
// so every comparison marks all sixteen positions before deciding.
func TestWorstCaseWrites(t *testing.T) {
	transitions := loadMachine(t)
	for n := 2; n <= 16; n++ {
		words := make([]string, n)
		for i := range words {
			words[i] = strings.Repeat("0", wordLength-1) + string(hexRunes[n-1-i])
		}
		t.Run(fmt.Sprintf("n_%d", n), func(t *testing.T) {
			assertSorts(t, transitions, words)
			result := runMachine(t, transitions, encode(words))
			t.Logf("n=%d changed writes=%d", n, result.changedWrites)
		})
	}
}

// Already sorted input should finish after one pass with no swaps. Each
// comparison only marks and unmarks digits (at most 64 writes), so the total is
// linear in n rather than quadratic.
func TestAlreadySortedIsCheap(t *testing.T) {
	transitions := loadMachine(t)
	words := make([]string, 10)
	for i := range words {
		words[i] = strings.Repeat("0", wordLength-1) + string(hexRunes[i])
	}
	result := runMachine(t, transitions, encode(words))
	assertSorts(t, transitions, words)
	if limit := int64(64) * int64(len(words)-1); result.changedWrites > limit {
		t.Fatalf("sorted input used %d changed writes, want at most %d", result.changedWrites, limit)
	}
}

// Fixed-width uppercase hex has the same lexical and unsigned numeric order.
func TestNumericBoundariesAndDuplicates(t *testing.T) {
	transitions := loadMachine(t)
	cases := []struct {
		name  string
		words []string
	}{
		{
			name: "unsigned_boundary",
			words: []string{
				"FFFFFFFFFFFFFFFF", "8000000000000000",
				"0000000000000000", "7FFFFFFFFFFFFFFF",
			},
		},
		{
			name:  "carry_boundary",
			words: []string{"0000000100000000", "00000000FFFFFFFF"},
		},
		{
			name: "mixed_duplicates",
			words: []string{
				"8000000000000000", "0000000000000000", "FFFFFFFFFFFFFFFF",
				"8000000000000000", "FFFFFFFFFFFFFFFF", "0000000000000000",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSorts(t, transitions, tc.words)
		})
	}
}
