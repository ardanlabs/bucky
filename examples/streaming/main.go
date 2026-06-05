// streaming demonstrates pseudo-streaming transcription by sliding a
// fixed-size window across the input audio. Each window is decoded with
// whisper.Full, and the tail of the previous window's tokens is fed back as
// PromptTokens so the next window has linguistic context.
//
// Real real-time streaming is application-specific (microphone capture, VAD
// gating, push notifications); this example focuses on carrying decoder
// context across windows using whisper.StringRefs.SetPromptTokens, which
// copies the tail tokens into a GC-safe buffer and keeps them alive for the
// duration of the Full call.
//
// Usage:
//
//	BUCKY_LIB=./lib BUCKY_TEST_MODEL=$HOME/models/ggml-tiny.bin \
//	    go run ./examples/streaming samples/jfk.wav
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ardanlabs/bucky/pkg/audio"
	"github.com/ardanlabs/bucky/pkg/whisper"
)

func main() {
	var (
		windowSec  = flag.Float64("window", 10.0, "decode window size in seconds (5-10 typical)")
		overlapSec = flag.Float64("overlap", 1.0, "overlap between consecutive windows in seconds")
		nPromptTok = flag.Int("prompt-tokens", 64, "max tail tokens to carry into the next window")
		threads    = flag.Int("threads", 4, "number of CPU threads")
	)
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatalf("usage: %s [flags] <audio-file>", os.Args[0])
	}
	audioPath := flag.Arg(0)

	libPath := os.Getenv("BUCKY_LIB")
	if libPath == "" {
		log.Fatal("BUCKY_LIB must point to the directory containing libwhisper")
	}
	modelPath := os.Getenv("BUCKY_TEST_MODEL")
	if modelPath == "" {
		log.Fatal("BUCKY_TEST_MODEL must point to a GGML whisper model")
	}

	if err := whisper.Load(libPath); err != nil {
		log.Fatalf("whisper.Load: %v", err)
	}

	if err := whisper.Init(libPath); err != nil {
		log.Fatalf("whisper.Init: %v", err)
	}

	cparams := whisper.ContextDefaultParams()
	ctx, err := whisper.InitFromFileWithParams(modelPath, cparams)
	if err != nil {
		log.Fatalf("InitFromFileWithParams: %v", err)
	}
	defer whisper.Free(ctx)

	samples, err := loadAudio(audioPath)
	if err != nil {
		log.Fatal(err)
	}

	text, err := streamDecode(ctx, samples, *windowSec, *overlapSec, *nPromptTok, *threads)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(strings.TrimSpace(text))
}

func loadAudio(audioPath string) ([]float32, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", audioPath, err)
	}
	defer f.Close()
	samples, err := audio.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("audio.Decode: %w", err)
	}
	return samples, nil
}

func streamDecode(ctx whisper.Context, samples []float32, windowSec, overlapSec float64, nPromptTok, threads int) (string, error) {
	winSamples := int(windowSec * float64(whisper.SampleRate))
	overSamples := int(overlapSec * float64(whisper.SampleRate))
	if overSamples >= winSamples {
		return "", fmt.Errorf("overlap (%ds) must be smaller than window (%ds)", int(overlapSec), int(windowSec))
	}
	step := winSamples - overSamples

	var (
		out         strings.Builder
		promptToks  []whisper.Token // tail tokens from the previous window
		windowIndex int
	)

	for start := 0; start < len(samples); start += step {
		end := min(start+winSamples, len(samples))

		nextPrompt, err := decodeWindow(ctx, samples[start:end], promptToks, threads, &out)
		if err != nil {
			return "", fmt.Errorf("full window %d: %w", windowIndex, err)
		}
		if len(nextPrompt) > nPromptTok {
			nextPrompt = nextPrompt[len(nextPrompt)-nPromptTok:]
		}

		promptToks = nextPrompt
		windowIndex++

		if end == len(samples) {
			break
		}
	}

	return out.String(), nil
}

// decodeWindow runs whisper.Full on a single window, appends its text to out,
// and returns the window's tokens for use as the next window's PromptTokens.
//
// SetPromptTokens copies the previous window's tail tokens into a buffer
// owned by refs and hands whisper.cpp a const whisper_token * into it; the
// deferred refs.KeepAlive keeps that buffer reachable for the duration of
// the Full call. NoContext=1 ensures whisper does not also prepend its own
// previous-segment context.
func decodeWindow(ctx whisper.Context, chunk []float32, promptToks []whisper.Token, threads int, out *strings.Builder) ([]whisper.Token, error) {
	wparams := whisper.FullDefaultParams(whisper.SamplingGreedy)
	wparams.NThreads = int32(threads)
	wparams.PrintProgress = 0
	wparams.PrintRealtime = 0
	wparams.PrintTimestamps = 0
	wparams.NoTimestamps = 1
	wparams.SingleSegment = 1
	wparams.NoContext = 1 // we manage context explicitly via PromptTokens

	var refs whisper.StringRefs
	refs.SetPromptTokens(&wparams, promptToks)
	defer refs.KeepAlive()

	if err := whisper.Full(ctx, wparams, chunk); err != nil {
		return nil, err
	}

	var nextPrompt []whisper.Token
	for i := int32(0); i < whisper.FullNSegments(ctx); i++ {
		out.WriteString(whisper.FullGetSegmentText(ctx, i))
		for j := int32(0); j < whisper.FullNTokens(ctx, i); j++ {
			id := whisper.FullGetTokenID(ctx, i, j)
			if id == whisper.TokenNull {
				continue
			}
			nextPrompt = append(nextPrompt, id)
		}
	}
	return nextPrompt, nil
}
