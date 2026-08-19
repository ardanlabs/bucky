package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ardanlabs/bucky/pkg/audio"
	"github.com/ardanlabs/bucky/pkg/whisper"
	"github.com/urfave/cli/v2"
)

// WhisperCmd groups whisper subcommands under "bucky whisper".
var WhisperCmd = &cli.Command{
	Name:  "whisper",
	Usage: "Run whisper.cpp commands (transcribe, translate, segments)",
	Subcommands: []*cli.Command{
		whisperTranscribeCmd,
	},
}

var whisperTranscribeCmd = &cli.Command{
	Name:      "transcribe",
	Usage:     "Transcribe an audio file (WAV / MP3 / FLAC)",
	ArgsUsage: "<audio-file>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "lib",
			Aliases: []string{"l"},
			Usage:   "path to whisper.cpp compiled library files",
			EnvVars: []string{"BUCKY_LIB"},
		},
		&cli.StringFlag{
			Name:     "model",
			Aliases:  []string{"m"},
			Usage:    "path to a GGML whisper model (e.g. ggml-tiny.bin)",
			EnvVars:  []string{"BUCKY_MODEL", "BUCKY_TEST_MODEL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:  "lang",
			Usage: "language code (e.g. \"en\"); empty = auto-detect",
			Value: "",
		},
		&cli.BoolFlag{
			Name:  "translate",
			Usage: "translate to English",
		},
		&cli.StringFlag{
			Name:  "prompt",
			Usage: "initial prompt to bias the decoder",
		},
		&cli.IntFlag{
			Name:  "threads",
			Usage: "number of CPU threads",
			Value: 4,
		},
		&cli.IntFlag{
			Name:  "beam",
			Usage: "beam size (>0 enables beam-search sampling)",
			Value: 0,
		},
		&cli.IntFlag{
			Name:        "best-of",
			Usage:       "number of independent decoders used for greedy sampling",
			DefaultText: "whisper.cpp default",
		},
		&cli.Float64Flag{
			Name:        "temperature",
			Usage:       "initial sampling temperature (0 = deterministic)",
			DefaultText: "whisper.cpp default",
		},
		&cli.Float64Flag{
			Name:        "temperature-inc",
			Usage:       "temperature increase for fallback decoding (0 disables fallback)",
			DefaultText: "whisper.cpp default",
		},
		&cli.Float64Flag{
			Name:        "entropy-thold",
			Usage:       "entropy threshold for decoder fallback",
			DefaultText: "whisper.cpp default",
		},
		&cli.Float64Flag{
			Name:        "logprob-thold",
			Usage:       "average log probability threshold for decoder fallback",
			DefaultText: "whisper.cpp default",
		},
		&cli.Float64Flag{
			Name:        "no-speech-thold",
			Usage:       "no-speech probability threshold",
			DefaultText: "whisper.cpp default",
		},
		&cli.Float64Flag{
			Name:        "length-penalty",
			Usage:       "sequence length penalty used when ranking candidates",
			DefaultText: "whisper.cpp default",
		},
		&cli.Float64Flag{
			Name:        "max-initial-ts",
			Usage:       "maximum initial timestamp in seconds",
			DefaultText: "whisper.cpp default",
		},
		&cli.IntFlag{
			Name:        "max-tokens",
			Usage:       "maximum tokens per segment (0 = no limit)",
			DefaultText: "whisper.cpp default",
		},
		&cli.BoolFlag{
			Name:  "segments",
			Usage: "print one line per segment with timestamps",
		},
	},
	Action: runWhisperTranscribe,
}

func runWhisperTranscribe(c *cli.Context) error {
	if c.NArg() < 1 {
		return errors.New("expected an audio file path")
	}
	audioPath := c.Args().First()
	libPath := c.String("lib")
	modelPath := c.String("model")
	if libPath == "" {
		return errors.New("missing --lib or BUCKY_LIB")
	}

	if err := whisper.Load(libPath); err != nil {
		return fmt.Errorf("whisper.Load: %w", err)
	}

	if err := whisper.Init(libPath); err != nil {
		return fmt.Errorf("whisper.Init: %w", err)
	}

	cparams := whisper.ContextDefaultParams()
	ctx, err := whisper.InitFromFileWithParams(modelPath, cparams)
	if err != nil {
		return fmt.Errorf("InitFromFileWithParams: %w", err)
	}
	defer whisper.Free(ctx)

	samples, err := decodeAudioFile(audioPath)
	if err != nil {
		return err
	}

	wparams := buildTranscribeParams(c)

	var refs whisper.StringRefs
	if err := refs.SetLanguage(&wparams, c.String("lang")); err != nil {
		return err
	}
	if err := refs.SetInitialPrompt(&wparams, c.String("prompt")); err != nil {
		return err
	}
	defer refs.KeepAlive()

	if err := whisper.Full(ctx, wparams, samples); err != nil {
		return err
	}

	return printTranscribeResult(ctx, c.Bool("segments"))
}

func decodeAudioFile(audioPath string) ([]float32, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	samples, err := audio.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("audio.Decode: %w", err)
	}
	return samples, nil
}

func buildTranscribeParams(c *cli.Context) whisper.WhisperFullParams {
	strategy := whisper.SamplingGreedy
	if c.Int("beam") > 0 {
		strategy = whisper.SamplingBeamSearch
	}
	wparams := whisper.FullDefaultParams(strategy)
	applyTranscribeParams(c, &wparams)
	return wparams
}

func applyTranscribeParams(c *cli.Context, wparams *whisper.WhisperFullParams) {
	wparams.NThreads = int32(c.Int("threads"))
	if c.Int("beam") > 0 {
		wparams.BeamSearchBeamSize = int32(c.Int("beam"))
	}
	if c.IsSet("best-of") {
		wparams.GreedyBestOf = int32(c.Int("best-of"))
	}
	if c.IsSet("temperature") {
		wparams.Temperature = float32(c.Float64("temperature"))
	}
	if c.IsSet("temperature-inc") {
		wparams.TemperatureInc = float32(c.Float64("temperature-inc"))
	}
	if c.IsSet("entropy-thold") {
		wparams.EntropyThold = float32(c.Float64("entropy-thold"))
	}
	if c.IsSet("logprob-thold") {
		wparams.LogprobThold = float32(c.Float64("logprob-thold"))
	}
	if c.IsSet("no-speech-thold") {
		wparams.NoSpeechThold = float32(c.Float64("no-speech-thold"))
	}
	if c.IsSet("length-penalty") {
		wparams.LengthPenalty = float32(c.Float64("length-penalty"))
	}
	if c.IsSet("max-initial-ts") {
		wparams.MaxInitialTS = float32(c.Float64("max-initial-ts"))
	}
	if c.IsSet("max-tokens") {
		wparams.MaxTokens = int32(c.Int("max-tokens"))
	}
	if c.Bool("translate") {
		wparams.Translate = 1
	}
	wparams.PrintProgress = 0
	wparams.PrintRealtime = 0
	wparams.PrintTimestamps = 0
	if !c.Bool("segments") {
		wparams.NoTimestamps = 1
	}
}

func printTranscribeResult(ctx whisper.Context, segments bool) error {
	if segments {
		for i := int32(0); i < whisper.FullNSegments(ctx); i++ {
			t0 := whisper.FullGetSegmentT0(ctx, i) * 10
			t1 := whisper.FullGetSegmentT1(ctx, i) * 10
			fmt.Printf("[%s -> %s] %s\n",
				formatMs(t0), formatMs(t1),
				whisper.FullGetSegmentText(ctx, i),
			)
		}
		return nil
	}

	var sb strings.Builder
	for i := int32(0); i < whisper.FullNSegments(ctx); i++ {
		sb.WriteString(whisper.FullGetSegmentText(ctx, i))
	}
	fmt.Println(strings.TrimSpace(sb.String()))
	return nil
}

func formatMs(ms int64) string {
	s := ms / 1000
	mm := s / 60
	ss := s % 60
	return fmt.Sprintf("%02d:%02d.%03d", mm, ss, ms%1000)
}
