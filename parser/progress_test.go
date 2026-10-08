package parser

import (
	"fmt"
	"testing"

	"github.com/agentstation/vhs/lexer"
)

func TestProgressBarRejectsNonHexColors(t *testing.T) {
	for _, color := range []string{"red", `red"broken`, "", "#", "#1234", "#+12", "#12-", "#GGG", "#1234567", "#123456789", "#１２３", "#fff\n"} {
		t.Run(color, func(t *testing.T) {
			p := New(lexer.New(fmt.Sprintf("Set ProgressBar '%s'", color)))
			_ = p.Parse()
			if len(p.Errors()) == 0 {
				t.Fatalf("invalid progress color %q accepted", color)
			}
		})
	}
}

func TestProgressBarPreservesHexColors(t *testing.T) {
	for _, color := range []string{"#000", "#fAb", "#Ab12eF", "#00000000", "#aB12Cd80"} {
		t.Run(color, func(t *testing.T) {
			p := New(lexer.New(fmt.Sprintf("Set ProgressBar '%s'", color)))
			cmds := p.Parse()
			if len(p.Errors()) != 0 || len(cmds) != 1 || cmds[0].Args != color {
				t.Fatalf("color %q: commands=%v errors=%v", color, cmds, p.Errors())
			}
		})
	}
}

func TestProgressBarRequiresColor(t *testing.T) {
	p := New(lexer.New("Set ProgressBar"))
	_ = p.Parse()
	if len(p.Errors()) == 0 {
		t.Fatal("missing color accepted")
	}
}
