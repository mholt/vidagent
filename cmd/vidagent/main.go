package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"
)

var (
	inputFile, outputFile, filterFile string
	overwrite                         bool
)

func init() {
	flag.StringVar(&inputFile, "in", inputFile, "the input file")
	flag.StringVar(&outputFile, "out", outputFile, "the output file")
	flag.StringVar(&filterFile, "filter", filterFile, "the filter file")
	flag.BoolVar(&overwrite, "f", overwrite, "force overwrite of output file if it exists")
}

func main() {
	flag.Parse()

	if inputFile == "" {
		log.Fatal("input file required (use -in)")
	}
	if outputFile == "" {
		log.Fatal("output file required (use -out)")
	}
	if filterFile == "" {
		log.Fatal("filter file required (use -filter)")
	}

	file, err := os.Open(filterFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	tokens, err := getTokens(file)
	if err != nil {
		log.Fatal(err)
	}

	actions, err := getActions(tokens)
	if err != nil {
		log.Fatal(err)
	}

	err = validateSegmentTimes(actions)
	if err != nil {
		log.Fatal(err)
	}

	filterCplx, err := buildComplexFilter(actions)
	if err != nil {
		log.Fatal(err)
	}

	ffmpegOverwriteOutput := "-n"
	if overwrite {
		ffmpegOverwriteOutput = "-y"
	}

	// order of arguments is important!
	// input 0 is the video file
	// input 1 is the null audio source (silence)
	// these correspond to values in the complex filter!
	args := []string{
		ffmpegOverwriteOutput,
		"-i", inputFile,
		"-f", "lavfi",
		"-i", "anullsrc",
		"-filter_complex", filterCplx,
		"-map", "[outv]",
		"-map", "[outa]",
		outputFile,
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
}

func getTokens(input io.Reader) ([]token, error) {
	var tokens []token
	scanner := bufio.NewScanner(input)

nextLine:
	for lineNum := 1; scanner.Scan(); lineNum += 1 {
		line := []rune(scanner.Text())

		tkn := token{linePos: lineNum, charPos: -1}
		saveTkn := func(charNum int) {
			tkn.val = strings.TrimSpace(tkn.val)
			tokens = append(tokens, tkn)
			tkn = token{linePos: lineNum, charPos: -1}
		}

		field := "verb"

		for charNum, ch := range line {
			isSpace := unicode.IsSpace(ch)

			if ch == '#' {
				continue nextLine
			}

			switch field {
			case "verb":
				if isSpace && tkn.val != "" {
					saveTkn(charNum)
					field = "start"
					continue
				}
			case "start":
				if ch == '-' && tkn.val != "" {
					saveTkn(charNum)
					field = "end"
					continue
				}
			case "end":
				if ch == '(' && tkn.val != "" {
					saveTkn(charNum)
					field = "reason"
					continue
				}
			case "reason":
				if ch == ')' {
					saveTkn(charNum)
					continue nextLine
				}
			default:
				return tokens, fmt.Errorf("unexpected state")
			}

			if tkn.val == "" && isSpace {
				continue
			}

			if tkn.charPos == -1 {
				tkn.charPos = charNum + 1
			}

			tkn.val += string(ch)
		}

		if tkn.val != "" {
			tkn.val = strings.TrimSpace(tkn.val)
			tokens = append(tokens, tkn)
		}
	}

	tokens = append(tokens)

	return tokens, scanner.Err()
}

func getActions(tokens []token) ([]action, error) {
	var actions []action
	line := 1

	var act action
	for _, tkn := range tokens {
		if tkn.linePos > line {
			if len(act.tokens) > 0 {
				actions = append(actions, act)
			}
			act = action{}
			line = tkn.linePos
		}

		switch len(act.tokens) {
		case 0:
			// verb
			verb, ok := verbs[strings.ToLower(tkn.val)]
			if !ok {
				return actions, fmt.Errorf("line %d:%d: unrecognized verb '%s'",
					tkn.linePos, tkn.charPos, tkn.val)
			}
			act.verb = verb
		case 1:
			// start time
			startTime, err := ParseTime(tkn.val)
			if err != nil {
				return actions, fmt.Errorf("invalid start time: %v", err)
			}
			act.start = startTime
		case 2:
			// end time
			endTime, err := ParseTime(tkn.val)
			if err != nil {
				return actions, fmt.Errorf("invalid end time: %v", err)
			}
			act.end = endTime
		case 3:
			// reason
			rsn, err := ParseReason(tkn.val)
			if err != nil {
				return actions, fmt.Errorf("invalid reason value: %v", err)
			}
			act.reason = rsn
		default:
			return actions, fmt.Errorf("line %d: unexpected token count of %d",
				tkn.linePos, len(act.tokens))
		}
		act.tokens = append(act.tokens, tkn)
	}

	if len(act.tokens) > 0 {
		actions = append(actions, act)
	}

	return actions, nil
}

func validateSegmentTimes(actions []action) error {
	for i, act := range actions {
		if len(act.tokens) == 0 {
			return fmt.Errorf("action %d: no tokens", i)
		}
		if act.end.SecondNum() < act.start.SecondNum() {
			return fmt.Errorf("line %d: end time %s comes before start time %s",
				act.tokens[0].linePos, act.end, act.start)
		}
		threshold := .001
		if act.end.SecondNum()-act.start.SecondNum() < threshold {
			return fmt.Errorf("line %d: start time %s and end time %s are too close; within %f of each other",
				act.tokens[0].linePos, act.end, act.start, threshold)
		}
		if i > 0 {
			if actions[i].end.SecondNum() < actions[i-1].start.SecondNum() {
				return fmt.Errorf("lines %d-%d: segments are out of order",
					actions[i-1].tokens[0].linePos, act.tokens[0].linePos)
			}
			if actions[i].start.SecondNum()-actions[i-1].end.SecondNum() < threshold {
				return fmt.Errorf("lines %d-%d: segments overlap or are too close",
					actions[i-1].tokens[0].linePos, act.tokens[0].linePos)
			}
		}
	}
	return nil
}

func buildComplexFilter(actions []action) (string, error) {
	if len(actions) == 0 {
		return "", fmt.Errorf("no actions to perform")
	}

	var s string
	var segmentCounter int

	vidSegment := func() string { return fmt.Sprintf("video%d", segmentCounter) }
	audSegment := func() string { return fmt.Sprintf("audio%d", segmentCounter) }
	prevVidSegment := func(n int) string { return fmt.Sprintf("video%d", segmentCounter+n) }
	prevAudSegment := func(n int) string { return fmt.Sprintf("audio%d", segmentCounter+n) }

	// beginning of video
	firstSec := actions[0].start.SecondString()
	s += fmt.Sprintf("[0:v]trim=duration=%s[%s];[0:a]atrim=duration=%s[%s];",
		firstSec, vidSegment(), firstSec, audSegment())

	// trim for each action
	for i, act := range actions {
		switch act.verb {
		case CutVerb:
			// cut out this segment by splicing in the segments around it,
			// concatenating as we go (concats are themselves new segments)
			if i > 0 {
				// before it
				segmentCounter++
				s += fmt.Sprintf("[0:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS[%s];[0:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS[%s];",
					actions[i-1].end.SecondString(), act.start.SecondString(), vidSegment(),
					actions[i-1].end.SecondString(), act.start.SecondString(), audSegment())
				segmentCounter++
				s += fmt.Sprintf("[%s][%s]concat[%s];[%s][%s]concat=v=0:a=1[%s];",
					prevVidSegment(-2), prevVidSegment(-1), vidSegment(),
					prevAudSegment(-2), prevAudSegment(-1), audSegment())
			}
			if i < len(actions)-1 && actions[i+1].verb != CutVerb {
				// after it
				segmentCounter++
				s += fmt.Sprintf("[0:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS[%s];[0:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS[%s];",
					act.end.SecondString(), actions[i+1].start.SecondString(), vidSegment(),
					act.end.SecondString(), actions[i+1].start.SecondString(), audSegment())
				segmentCounter++
				s += fmt.Sprintf("[%s][%s]concat[%s];[%s][%s]concat=v=0:a=1[%s];",
					prevVidSegment(-2), prevVidSegment(-1), vidSegment(),
					prevAudSegment(-2), prevAudSegment(-1), audSegment())
			}

		case MuteVerb:
			// mute this segment
			segmentCounter++
			s += fmt.Sprintf("[0:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS[%s];[1:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS[%s];",
				act.start.SecondString(), act.end.SecondString(), vidSegment(),
				act.start.SecondString(), act.end.SecondString(), audSegment())

			// concatenate segments; this is itself a new segment
			segmentCounter++
			s += fmt.Sprintf("[%s][%s]concat[%s];[%s][%s]concat=v=0:a=1[%s];",
				prevVidSegment(-2), prevVidSegment(-1), vidSegment(),
				prevAudSegment(-2), prevAudSegment(-1), audSegment())

		default:
			return s, fmt.Errorf("action %d: unsupported verb '%s'", i, act.verb)
		}
	}

	// end of video
	lastAction := actions[len(actions)-1]
	segmentCounter++
	s += fmt.Sprintf("[0:v]trim=start=%s,setpts=PTS-STARTPTS[%s];[0:a]atrim=start=%s,asetpts=PTS-STARTPTS[%s];",
		lastAction.end.SecondString(), vidSegment(),
		lastAction.end.SecondString(), audSegment())

	// concatenate final output segment
	s += fmt.Sprintf("[%s][%s]concat[%s];[%s][%s]concat=v=0:a=1[%s]",
		prevVidSegment(-1), vidSegment(), "outv",
		prevAudSegment(-1), audSegment(), "outa")

	return s, nil
}

type token struct {
	val     string
	linePos int
	charPos int
}

type action struct {
	tokens []token
	verb   Verb
	start  Time
	end    Time
	reason Reason
}

type Verb string

const (
	CutVerb  Verb = "cut"
	MuteVerb      = "mute"
)

type Time struct {
	Hour   int
	Minute int
	Second float64
}

func (t Time) String() string {
	if t.Hour > 0 {
		return fmt.Sprintf("%d:%d:%2.2f", t.Hour, t.Minute, t.Second)
	}
	return fmt.Sprintf("%d:%2.2f", t.Minute, t.Second)
}

func (t Time) SecondString() string {
	return fmt.Sprintf("%.2f", t.SecondNum())
}

func (t Time) SecondNum() float64 {
	return float64(t.Hour*60*60+t.Minute*60) + t.Second
}

func ParseTime(timeStr string) (Time, error) {
	timeStr = strings.TrimSpace(timeStr)

	if timeStr == "" {
		return Time{}, nil
	}

	parts := strings.Split(timeStr, ":")
	for i := range parts {
		if parts[i] == "" {
			parts[i] = "00"
		}
	}

	var hour, min int
	var sec float64
	var err error

	switch len(parts) {
	case 1:
		sec, err = strconv.ParseFloat(parts[0], 32)
	case 2:
		min, err = strconv.Atoi(parts[0])
		if err != nil {
			return Time{}, fmt.Errorf("bad minute value %s: %v", parts[0], err)
		}
		sec, err = strconv.ParseFloat(parts[1], 32)
	case 3:
		hour, err = strconv.Atoi(parts[0])
		if err != nil {
			return Time{}, fmt.Errorf("bad hour value %s: %v", parts[0], err)
		}
		min, err = strconv.Atoi(parts[1])
		if err != nil {
			return Time{}, fmt.Errorf("bad minute value %s: %v", parts[1], err)
		}
		sec, err = strconv.ParseFloat(parts[2], 32)
	default:
		return Time{}, fmt.Errorf("bad time format '%s'", timeStr)
	}
	if err != nil {
		return Time{}, fmt.Errorf("bad second value %s: %v", parts[len(parts)-1], err)
	}

	return Time{Hour: hour, Minute: min, Second: sec}, nil
}

type Reason struct {
	Category  string
	Specifier string
}

func ParseReason(reasonStr string) (Reason, error) {
	reasonStr = strings.TrimSpace(reasonStr)

	if reasonStr == "" {
		return Reason{}, nil
	}

	parts := strings.Split(reasonStr, ":")

	// TODO: validate the category and specifier strings to be within a known set?

	switch len(parts) {
	case 1:
		return Reason{Category: strings.TrimSpace(parts[0])}, nil
	case 2:
		return Reason{
			Category:  strings.TrimSpace(parts[0]),
			Specifier: strings.TrimSpace(parts[1]),
		}, nil
	default:
		return Reason{}, fmt.Errorf("bad reason format '%s'", reasonStr)
	}
}

var verbs = map[string]Verb{
	"cut":  CutVerb,
	"mute": MuteVerb,
}
