package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/agentstation/vhs/lexer"
	"github.com/agentstation/vhs/parser"
	"github.com/agentstation/vhs/token"
	"github.com/go-rod/rod"
)

// EvaluatorOption is a function that can be used to modify the VHS instance.
type EvaluatorOption func(*VHS)

// WithSVGOptimization returns an EvaluatorOption that sets the SVG optimization flag.
func WithSVGOptimization(optimize bool) EvaluatorOption {
	return func(v *VHS) {
		v.Options.SVG.OptimizeSize = optimize
	}
}

// WithDebugConsole returns an EvaluatorOption that enables browser console logging.
func WithDebugConsole(debug bool) EvaluatorOption {
	return func(v *VHS) {
		v.Options.DebugConsole = debug
	}
}

// WithBrowserPath selects the executable used for capture.
func WithBrowserPath(path string) EvaluatorOption {
	return func(v *VHS) {
		v.Options.BrowserPath = path
	}
}

// WithSVGFontFile selects an exact font for capture and SVG output.
func WithSVGFontFile(path string) EvaluatorOption {
	return func(v *VHS) {
		v.Options.SVG.FontFile = path
	}
}

// Evaluate takes as input a tape string, an output writer, and an output file
// and evaluates all the commands within the tape string and produces a GIF.
func Evaluate(ctx context.Context, tape string, out io.Writer, opts ...EvaluatorOption) []error {
	l := lexer.New(tape)
	p := parser.New(l)

	cmds := p.Parse()
	errs := p.Errors()
	if len(errs) != 0 || len(cmds) == 0 {
		return []error{InvalidSyntaxError{errs}}
	}

	v := New()

	// Apply evaluator options early (before Start) for options like DebugConsole
	// that need to be set before the browser is initialized
	for _, opt := range opts {
		opt(&v)
	}

	for _, cmd := range cmds {
		if cmd.Type == token.SET && cmd.Options == shellSetting || cmd.Type == token.ENV {
			err := Execute(cmd, &v)
			if err != nil {
				return []error{err}
			}
		}
	}

	// Start things up
	if err := v.Start(ctx); err != nil {
		return []error{err}
	}
	defer func() { _ = v.close() }()

	// Let's wait until we can access the window.term variable.
	//
	// This is necessary because some SET commands modify the terminal.
	err := v.Page.Wait(rod.Eval("() => window.term != undefined"))
	if err != nil {
		return []error{err}
	}

	var offset int
	for i, cmd := range cmds {
		if cmd.Type == token.SET || cmd.Type == token.OUTPUT || cmd.Type == token.REQUIRE {
			_, _ = fmt.Fprintln(out, Highlight(cmd, false))
			if cmd.Options != shellSetting {
				err := Execute(cmd, &v)
				if err != nil {
					return []error{err}
				}
			}
		} else {
			offset = i
			break
		}
	}

	// Make sure image is big enough to fit padding, bar, and margins
	video := v.Options.Video
	minWidth := double(video.Style.Padding) + double(video.Style.Margin)
	minHeight := double(video.Style.Padding) + double(video.Style.Margin)
	if video.Style.WindowBar != "" {
		minHeight += video.Style.WindowBarSize
	}
	if video.Style.Height < minHeight || video.Style.Width < minWidth {
		//nolint:staticcheck
		v.Errors = append(
			v.Errors,
			fmt.Errorf(
				"Dimensions must be at least %d x %d",
				minWidth, minHeight,
			),
		)
	}

	if len(v.Errors) > 0 {
		return v.Errors
	}

	// Setup the terminal session so we can start executing commands.
	if err := v.installSVGFont(); err != nil {
		return []error{err}
	}
	if err := v.Setup(); err != nil {
		return []error{err}
	}

	// If the first command (after Settings and Outputs) is a Hide command, we can
	// begin executing the commands before we start recording to avoid capturing
	// any unwanted frames.
	if cmds[offset].Type == token.HIDE {
		for i, cmd := range cmds[offset:] {
			if cmd.Type == token.SHOW {
				offset += i
				break
			}
			_, _ = fmt.Fprintln(out, Highlight(cmd, true))
			err := Execute(cmd, &v)
			if err != nil {
				return []error{err}
			}
		}
	}

	// Begin recording frames as we are now in a recording state.
	recordCtx, cancel := context.WithCancel(ctx)
	ch := v.Record(recordCtx)

	// Clean up temporary files at the end.
	defer func() {
		if v.Options.Video.Output.Frames != "" {
			// Move the frames to the output directory.
			_ = os.Rename(v.Options.Video.Input, v.Options.Video.Output.Frames)
		}

		_ = v.Cleanup()
	}()

	teardown := func() []error {
		cancel()
		var errs []error
		for err := range ch {
			errs = append(errs, err)
		}
		// Commands have stopped, so browser cleanup cannot interrupt their rod calls.
		_ = v.terminate()
		return errs
	}

	for _, cmd := range cmds[offset:] {
		// A capture failure stops the tape before the next command.
		select {
		case err := <-ch:
			if err != nil {
				return append([]error{err}, teardown()...)
			}
		default:
		}
		if ctx.Err() != nil {
			return append(teardown(), ctx.Err())
		}

		// When changing the FontFamily, FontSize, LineHeight, Padding
		// The xterm.js canvas changes dimensions and causes FFMPEG to not work
		// correctly (specifically) with palettegen.
		// It will be possible to change settings on the fly in the future, but
		// it is currently not as it does not result in a proper render of the
		// GIF as the frame sequence will change dimensions. This is fixable.
		//
		// We should remove if isSetting statement.
		isSetting := cmd.Type == token.SET && cmd.Options != "TypingSpeed"

		if isSetting {
			fmt.Println(ErrorStyle.Render(fmt.Sprintf("WARN: 'Set %s %s' has been ignored. Move the directive to the top of the file.\nLearn more: https://github.com/agentstation/vhs#settings", cmd.Options, cmd.Args)))
		}
		if isSetting || cmd.Type == token.REQUIRE {
			_, _ = fmt.Fprintln(out, Highlight(cmd, true))
			continue
		}
		_, _ = fmt.Fprintln(out, Highlight(cmd, !v.isRecording() || cmd.Type == token.SHOW || cmd.Type == token.HIDE || isSetting))
		err := Execute(cmd, &v)
		if err != nil {
			return append(teardown(), err)
		}
	}

	// If running as an SSH server, the output file is a temporary file
	// to use for the output.
	//
	// We need to set the GIF file path before it is created but after all of
	// the settings and commands are executed. This is done in `serve.go`.
	//
	// Since the GIF creation is deferred, setting the output file here will
	// achieve what we want.
	for _, opt := range opts {
		opt(&v)
	}

	if errs := teardown(); len(errs) > 0 {
		return errs
	}
	if err := v.Render(ctx); err != nil {
		return []error{err}
	}
	return nil
}
