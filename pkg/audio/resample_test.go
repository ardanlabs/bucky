package audio

import (
	"math"
	"testing"
)

func TestDownmixToMono(t *testing.T) {
	t.Run("mono passthrough", func(t *testing.T) {
		in := []float32{0.1, 0.2, 0.3}
		got := DownmixToMono(in, 1)
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		if &got[0] != &in[0] {
			t.Errorf("mono should pass through without copy")
		}
	})
	t.Run("stereo average", func(t *testing.T) {
		// L,R interleaved: (1,-1) (0.5,0.5) (0,0)
		in := []float32{1, -1, 0.5, 0.5, 0, 0}
		got := DownmixToMono(in, 2)
		want := []float32{0, 0.5, 0}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if math.Abs(float64(got[i]-want[i])) > 1e-6 {
				t.Errorf("got[%d] = %f, want %f", i, got[i], want[i])
			}
		}
	})
}

func TestSplitChannels(t *testing.T) {
	t.Run("mono passthrough", func(t *testing.T) {
		in := []float32{0.1, 0.2, 0.3}
		got := SplitChannels(in, 1)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if &got[0][0] != &in[0] {
			t.Errorf("mono should pass through without copy")
		}
	})
	t.Run("stereo split", func(t *testing.T) {
		// L,R interleaved: (1,-1) (0.5,0.5) (0,0.25)
		in := []float32{1, -1, 0.5, 0.5, 0, 0.25}
		got := SplitChannels(in, 2)
		want := [][]float32{
			{1, 0.5, 0},
			{-1, 0.5, 0.25},
		}
		if len(got) != len(want) {
			t.Fatalf("channels = %d, want %d", len(got), len(want))
		}
		for c := range want {
			if len(got[c]) != len(want[c]) {
				t.Fatalf("channel %d len = %d, want %d", c, len(got[c]), len(want[c]))
			}
			for i := range want[c] {
				if math.Abs(float64(got[c][i]-want[c][i])) > 1e-6 {
					t.Errorf("got[%d][%d] = %f, want %f", c, i, got[c][i], want[c][i])
				}
			}
		}
	})
}

func TestResampleLinear(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		in := []float32{1, 2, 3, 4}
		got := ResampleLinear(in, 16000, 16000)
		if &got[0] != &in[0] {
			t.Errorf("same-rate resample should pass through without copy")
		}
	})
	t.Run("downsample 2x", func(t *testing.T) {
		in := []float32{0, 1, 2, 3, 4, 5, 6, 7}
		got := ResampleLinear(in, 32000, 16000)
		if len(got) != 4 {
			t.Fatalf("len = %d, want 4", len(got))
		}
		// Expect roughly even-indexed samples.
		want := []float32{0, 2, 4, 6}
		for i := range want {
			if math.Abs(float64(got[i]-want[i])) > 1e-6 {
				t.Errorf("got[%d] = %f, want %f", i, got[i], want[i])
			}
		}
	})
	t.Run("upsample 2x", func(t *testing.T) {
		in := []float32{0, 2, 4, 6}
		got := ResampleLinear(in, 16000, 32000)
		if len(got) != 8 {
			t.Fatalf("len = %d, want 8", len(got))
		}
		// Linearly interpolated: 0, 1, 2, 3, 4, 5, 6, 6 (last sample held).
		want := []float32{0, 1, 2, 3, 4, 5, 6, 6}
		for i := range want {
			if math.Abs(float64(got[i]-want[i])) > 1e-6 {
				t.Errorf("got[%d] = %f, want %f", i, got[i], want[i])
			}
		}
	})
}
