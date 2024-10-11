package term

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type ScreenBuf struct {
	writer io.Writer
	lines  []string
	height int
}

func NewScreenBuf(w io.Writer) *ScreenBuf {
	_, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		height = 24 // fallback to a common terminal height
	}

	// Subtract 2 from the height to account for the prompt and the input line
	height -= 2
	if height < 1 {
		height = 1
	}

	return &ScreenBuf{
		writer: w,
		lines:  make([]string, 0, height),
		height: height,
	}
}

func (sb *ScreenBuf) WriteLine(s string) {
	if len(sb.lines) == sb.height {
		// Buffer is full, scroll up
		fmt.Fprint(
			sb.writer,
			"\033[H",
		) // Move cursor to top-left corner
		fmt.Fprint(
			sb.writer,
			"\033[M",
		) // Delete the top line
		fmt.Fprint(
			sb.writer,
			"\033["+fmt.Sprint(sb.height-1)+";1H",
		) // Move cursor to last line
		fmt.Fprint(
			sb.writer,
			s,
		) // Write the new line

		// Update our buffer
		copy(sb.lines, sb.lines[1:])
		sb.lines[sb.height-1] = s
	} else {
		// Buffer is not full yet, just append
		sb.lines = append(sb.lines, s)
		fmt.Fprintln(sb.writer, s)
	}
}

func (sb *ScreenBuf) Clear() {
	fmt.Fprint(sb.writer, "\033[H") // Move cursor to top-left corner
	fmt.Fprint(sb.writer, "\033[J") // Clear screen from cursor down
	sb.lines = sb.lines[:0]
}

func (sb *ScreenBuf) String() string {
	return strings.Join(sb.lines, "\n")
}