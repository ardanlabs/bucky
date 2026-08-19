package cmd

import (
	"flag"
	"testing"

	"github.com/ardanlabs/bucky/pkg/whisper"
	"github.com/urfave/cli/v2"
)

func TestApplyTranscribeParamsPreservesSamplingDefaults(t *testing.T) {
	c := transcribeTestContext(t)
	wparams := whisper.WhisperFullParams{
		Temperature:    0.1,
		TemperatureInc: 0.2,
		EntropyThold:   2.4,
		LogprobThold:   -1.0,
		NoSpeechThold:  0.6,
		LengthPenalty:  -1.0,
		MaxInitialTS:   1.0,
		MaxTokens:      20,
		GreedyBestOf:   5,
	}
	want := wparams

	applyTranscribeParams(c, &wparams)

	if wparams.Temperature != want.Temperature {
		t.Errorf("Temperature: got %v, want %v", wparams.Temperature, want.Temperature)
	}
	if wparams.TemperatureInc != want.TemperatureInc {
		t.Errorf("TemperatureInc: got %v, want %v", wparams.TemperatureInc, want.TemperatureInc)
	}
	if wparams.EntropyThold != want.EntropyThold {
		t.Errorf("EntropyThold: got %v, want %v", wparams.EntropyThold, want.EntropyThold)
	}
	if wparams.LogprobThold != want.LogprobThold {
		t.Errorf("LogprobThold: got %v, want %v", wparams.LogprobThold, want.LogprobThold)
	}
	if wparams.NoSpeechThold != want.NoSpeechThold {
		t.Errorf("NoSpeechThold: got %v, want %v", wparams.NoSpeechThold, want.NoSpeechThold)
	}
	if wparams.LengthPenalty != want.LengthPenalty {
		t.Errorf("LengthPenalty: got %v, want %v", wparams.LengthPenalty, want.LengthPenalty)
	}
	if wparams.MaxInitialTS != want.MaxInitialTS {
		t.Errorf("MaxInitialTS: got %v, want %v", wparams.MaxInitialTS, want.MaxInitialTS)
	}
	if wparams.MaxTokens != want.MaxTokens {
		t.Errorf("MaxTokens: got %v, want %v", wparams.MaxTokens, want.MaxTokens)
	}
	if wparams.GreedyBestOf != want.GreedyBestOf {
		t.Errorf("GreedyBestOf: got %v, want %v", wparams.GreedyBestOf, want.GreedyBestOf)
	}
}

func TestApplyTranscribeParamsSetsSamplingFlags(t *testing.T) {
	c := transcribeTestContext(t,
		"--beam", "8",
		"--best-of", "3",
		"--temperature", "0.4",
		"--temperature-inc", "0.1",
		"--entropy-thold", "2.0",
		"--logprob-thold", "-0.8",
		"--no-speech-thold", "0.5",
		"--length-penalty", "1.2",
		"--max-initial-ts", "0.75",
		"--max-tokens", "64",
	)
	var wparams whisper.WhisperFullParams

	applyTranscribeParams(c, &wparams)

	if wparams.BeamSearchBeamSize != 8 {
		t.Errorf("BeamSearchBeamSize: got %d, want 8", wparams.BeamSearchBeamSize)
	}
	if wparams.GreedyBestOf != 3 {
		t.Errorf("GreedyBestOf: got %d, want 3", wparams.GreedyBestOf)
	}
	if wparams.Temperature != 0.4 {
		t.Errorf("Temperature: got %v, want 0.4", wparams.Temperature)
	}
	if wparams.TemperatureInc != 0.1 {
		t.Errorf("TemperatureInc: got %v, want 0.1", wparams.TemperatureInc)
	}
	if wparams.EntropyThold != 2.0 {
		t.Errorf("EntropyThold: got %v, want 2.0", wparams.EntropyThold)
	}
	if wparams.LogprobThold != -0.8 {
		t.Errorf("LogprobThold: got %v, want -0.8", wparams.LogprobThold)
	}
	if wparams.NoSpeechThold != 0.5 {
		t.Errorf("NoSpeechThold: got %v, want 0.5", wparams.NoSpeechThold)
	}
	if wparams.LengthPenalty != 1.2 {
		t.Errorf("LengthPenalty: got %v, want 1.2", wparams.LengthPenalty)
	}
	if wparams.MaxInitialTS != 0.75 {
		t.Errorf("MaxInitialTS: got %v, want 0.75", wparams.MaxInitialTS)
	}
	if wparams.MaxTokens != 64 {
		t.Errorf("MaxTokens: got %d, want 64", wparams.MaxTokens)
	}
}

func transcribeTestContext(t *testing.T, args ...string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("transcribe", flag.ContinueOnError)
	for _, f := range whisperTranscribeCmd.Flags {
		if err := f.Apply(set); err != nil {
			t.Fatalf("Apply flag: %v", err)
		}
	}
	if err := set.Parse(args); err != nil {
		t.Fatalf("Parse flags: %v", err)
	}

	return cli.NewContext(nil, set, nil)
}
