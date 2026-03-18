// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"golang.org/x/tools/cover"
)

// textConfig controls what the text formatter displays.
type textConfig struct {
	color   bool // use ANSI color sequences
	lines   bool // show line numbers
	hits    bool // show execution counts
	summary bool // summary table only, no source
}

// textOutput reads the profile data from profile and generates a
// plain-text coverage report, writing it to outfile.
// If outfile is empty, it writes to stdout.
func textOutput(profile, outfile string) error {
	profiles, err := cover.ParseProfiles(profile)
	if err != nil {
		return err
	}

	dirs, err := findPkgs(profiles)
	if err != nil {
		return err
	}

	var out io.Writer
	if outfile == "" {
		out = os.Stdout
	} else {
		f, err := os.Create(outfile)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	cfg := textConfig{
		color:   *textColor,
		lines:   *textLines,
		hits:    *textHits,
		summary: *textSummary,
	}

	isSet := false
	for _, p := range profiles {
		if p.Mode == "set" {
			isSet = true
			break
		}
	}

	// Print summary table.
	printSummaryTable(out, profiles, isSet, cfg)

	if cfg.summary {
		return nil
	}

	// Print legend.
	if cfg.color {
		fmt.Fprintln(out)
		printLegend(out, isSet)
	}
	fmt.Fprintln(out)

	// Print annotated source for each file.
	for i, p := range profiles {
		fn := p.FileName
		file, err := findFile(dirs, fn)
		if err != nil {
			return err
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("can't read %q: %v", fn, err)
		}
		var buf strings.Builder
		err = textGen(&buf, src, p.Boundaries(src), p, cfg)
		if err != nil {
			return err
		}

		// File marker.
		fmt.Fprintf(out, "--- file: %s (%.1f%%) ---\n", fn, percentCovered(p))
		fmt.Fprint(out, buf.String())
		if i < len(profiles)-1 {
			fmt.Fprintln(out)
		}
	}

	return nil
}

// printSummaryTable prints a coverage.py-style summary table.
func printSummaryTable(out io.Writer, profiles []*cover.Profile, isSet bool, cfg textConfig) {
	type fileSummary struct {
		name    string
		stmts   int64
		miss    int64
		cover   float64
		missing string
	}

	var summaries []fileSummary
	var totalStmts, totalMiss int64

	// Compute column widths.
	nameWidth := len("File")
	for _, p := range profiles {
		if len(p.FileName) > nameWidth {
			nameWidth = len(p.FileName)
		}

		var stmts, miss int64
		for _, b := range p.Blocks {
			stmts += int64(b.NumStmt)
			if b.Count == 0 {
				miss += int64(b.NumStmt)
			}
		}
		totalStmts += stmts
		totalMiss += miss

		summaries = append(summaries, fileSummary{
			name:    p.FileName,
			stmts:   stmts,
			miss:    miss,
			cover:   percentCovered(p),
			missing: missingLines(p),
		})
	}

	var totalCover float64
	if totalStmts > 0 {
		totalCover = float64(totalStmts-totalMiss) / float64(totalStmts) * 100
	}

	// Print header.
	sep := strings.Repeat("-", nameWidth+42)
	fmt.Fprintf(out, "%-*s    Stmts    Miss   Cover   Missing\n", nameWidth, "File")
	fmt.Fprintln(out, sep)

	for _, s := range summaries {
		coverStr := fmt.Sprintf("%.1f%%", s.cover)
		if cfg.color {
			coverStr = colorize(coverStr, coverLevel(s.cover))
		}
		fmt.Fprintf(out, "%-*s    %5d   %5d   %5s   %s\n",
			nameWidth, s.name, s.stmts, s.miss, coverStr, s.missing)
	}

	fmt.Fprintln(out, sep)
	totalCoverStr := fmt.Sprintf("%.1f%%", totalCover)
	if cfg.color {
		totalCoverStr = colorize(totalCoverStr, coverLevel(totalCover))
	}
	fmt.Fprintf(out, "%-*s    %5d   %5d   %5s\n",
		nameWidth, "TOTAL", totalStmts, totalMiss, totalCoverStr)
}

// coverLevel returns the ANSI color level (0-10) for a coverage percentage.
func coverLevel(pct float64) int {
	switch {
	case pct == 0:
		return 0
	case pct < 50:
		return 1
	case pct < 65:
		return 3
	case pct < 80:
		return 5
	case pct < 90:
		return 7
	default:
		return 10
	}
}

// colorize wraps s with an ANSI color for coverage level n and reset.
func colorize(s string, n int) string {
	return ansiCov(n) + s + ansiReset
}

// missingLines returns a string describing uncovered line ranges, e.g. "10-20, 35, 50-60".
func missingLines(p *cover.Profile) string {
	type lineRange struct{ start, end int }
	var uncovered []lineRange
	for _, b := range p.Blocks {
		if b.Count == 0 {
			uncovered = append(uncovered, lineRange{b.StartLine, b.EndLine})
		}
	}
	sort.Slice(uncovered, func(i, j int) bool { return uncovered[i].start < uncovered[j].start })

	// Merge overlapping/adjacent ranges.
	var merged []lineRange
	for _, r := range uncovered {
		if len(merged) > 0 && r.start <= merged[len(merged)-1].end+1 {
			if r.end > merged[len(merged)-1].end {
				merged[len(merged)-1].end = r.end
			}
		} else {
			merged = append(merged, r)
		}
	}

	parts := make([]string, len(merged))
	for i, r := range merged {
		if r.start == r.end {
			parts[i] = fmt.Sprintf("%d", r.start)
		} else {
			parts[i] = fmt.Sprintf("%d-%d", r.start, r.end)
		}
	}
	return strings.Join(parts, ", ")
}

// printLegend prints a color legend to out.
func printLegend(out io.Writer, isSet bool) {
	fmt.Fprintln(out, "Legend:")
	fmt.Fprintf(out, "  %s  not tracked\n", ansiRGB(80, 80, 80)+"not tracked"+ansiReset)
	if isSet {
		fmt.Fprintf(out, "  %s  not covered\n", ansiCov(0)+"not covered"+ansiReset)
		fmt.Fprintf(out, "  %s  covered\n", ansiCov(8)+"covered"+ansiReset)
	} else {
		fmt.Fprintf(out, "  %s  no coverage\n", ansiCov(0)+"no coverage"+ansiReset)
		fmt.Fprintf(out, "  %s  low coverage\n", ansiCov(1)+"low coverage"+ansiReset)
		fmt.Fprintf(out, "  %s  high coverage\n", ansiCov(10)+"high coverage"+ansiReset)
	}
}

const ansiReset = "\033[0m"

// ansiRGB returns an ANSI 24-bit foreground color escape sequence.
func ansiRGB(r, g, b int) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

// covRGB returns the r, g, b values for the specified coverage level
// between 0 (no coverage) and 10 (max coverage), matching the HTML formatter.
func covRGB(n int) (r, g, b int) {
	if n == 0 {
		return 192, 0, 0 // Red
	}
	// Gradient from gray to green, matching html.go rgb().
	r = 128 - 12*(n-1)
	g = 128 + 12*(n-1)
	b = 128 + 3*(n-1)
	return
}

// ansiCov returns the ANSI color escape sequence for coverage level n (0-10).
func ansiCov(n int) string {
	r, g, b := covRGB(n)
	return ansiRGB(r, g, b)
}

// buildLineMap builds a mapping from line number to execution count.
// Lines not in any block are absent from the map (-1 convention).
// Lines in a block with count 0 are uncovered; otherwise max count wins.
func buildLineMap(p *cover.Profile) map[int]int {
	m := make(map[int]int)
	for _, b := range p.Blocks {
		for line := b.StartLine; line <= b.EndLine; line++ {
			if count, ok := m[line]; !ok || b.Count > count {
				m[line] = b.Count
			}
		}
	}
	return m
}

// formatHitCount formats an execution count for the gutter.
const hitWidth = 7

func formatHitCount(count int) string {
	switch {
	case count >= 1_000_000:
		return fmt.Sprintf("%*.1fM", hitWidth-1, float64(count)/1e6)
	case count >= 100_000:
		return fmt.Sprintf("%*.1fk", hitWidth-1, float64(count)/1e3)
	default:
		return fmt.Sprintf("%*d", hitWidth, count)
	}
}

// textGen generates a plain-text coverage report with optional ANSI color
// sequences, line numbers, and execution counts in the left gutter.
func textGen(w io.Writer, src []byte, boundaries []cover.Boundary, p *cover.Profile, cfg textConfig) error {
	dst := bufio.NewWriter(w)
	lineMap := buildLineMap(p)
	totalLines := bytes.Count(src, []byte("\n")) + 1
	lineNumWidth := len(fmt.Sprintf("%d", totalLines))

	defaultColor := ""
	if cfg.color {
		defaultColor = ansiRGB(80, 80, 80)
	}
	currentColor := defaultColor

	lineNum := 1
	atLineStart := true

	for i := range src {
		// Emit gutter at the start of each line.
		if atLineStart {
			writeGutter(dst, lineNum, lineMap, lineNumWidth, cfg)
			if cfg.color {
				dst.WriteString(currentColor)
			}
			atLineStart = false
		}

		// Process boundaries at this byte offset.
		for len(boundaries) > 0 && boundaries[0].Offset == i {
			b := boundaries[0]
			if b.Start {
				n := 0
				if b.Count > 0 {
					n = int(math.Floor(b.Norm*9)) + 1
				}
				if cfg.color {
					c := ansiCov(n)
					dst.WriteString(c)
					currentColor = c
				}
			} else {
				if cfg.color {
					dst.WriteString(defaultColor)
				}
				currentColor = defaultColor
			}
			boundaries = boundaries[1:]
		}

		if src[i] == '\n' {
			if cfg.color {
				dst.WriteString(ansiReset)
			}
			dst.WriteByte('\n')
			lineNum++
			atLineStart = true
		} else {
			dst.WriteByte(src[i])
		}
	}

	// Handle last line if it doesn't end with newline.
	if !atLineStart {
		if cfg.color {
			dst.WriteString(ansiReset)
		}
		dst.WriteByte('\n')
	}

	return dst.Flush()
}

// writeGutter writes the left gutter (hit count and/or line number) for a line.
func writeGutter(dst *bufio.Writer, lineNum int, lineMap map[int]int, lineNumWidth int, cfg textConfig) {
	if cfg.hits {
		if count, ok := lineMap[lineNum]; ok {
			s := formatHitCount(count)
			if cfg.color {
				if count == 0 {
					dst.WriteString(ansiCov(0))
				} else {
					dst.WriteString(ansiRGB(100, 100, 100))
				}
				dst.WriteString(s)
				dst.WriteString(ansiReset)
			} else {
				dst.WriteString(s)
			}
		} else {
			fmt.Fprintf(dst, "%*s", hitWidth, "")
		}
		dst.WriteString("| ")
	}
	if cfg.lines {
		fmt.Fprintf(dst, "%*d| ", lineNumWidth, lineNum)
	}
}
