package audio

import (
	"math"
	"testing"
)

func TestResampler_IdentityPassthrough(t *testing.T) {
	in := []float32{1, 2, 3, 4}
	r := NewResampler(16000, 16000)
	got := r.Process(in)
	if &got[0] != &in[0] {
		t.Errorf("same-rate Process should pass through without copy")
	}
}

func TestResampler_EmptyBlock(t *testing.T) {
	r := NewResampler(48000, 16000)
	if got := r.Process(nil); got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
	if got := r.Process([]float32{}); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}

// TestResampler_BlockingMatchesOneShot is the core guarantee: feeding the
// signal in arbitrary block sizes produces (nearly) the same samples as
// resampling the whole signal at once. Small boundary differences are allowed
// because the one-shot ResampleLinear holds its final sample while the
// streaming version interpolates into the next block, but the bulk of the
// stream must match tightly and the counts must be within one sample.
func TestResampler_BlockingMatchesOneShot(t *testing.T) {
	const (
		inRate  = 44100
		outRate = 16000
		nIn     = 44100 // 1 second
	)

	// A smooth signal so linear interpolation is well-behaved and any phase
	// drift at block seams shows up clearly.
	in := make([]float32, nIn)
	for i := range in {
		in[i] = float32(math.Sin(2 * math.Pi * 220 * float64(i) / float64(inRate)))
	}

	for _, block := range []int{1, 7, 160, 441, 1024, nIn} {
		t.Run(blockName(block), func(t *testing.T) {
			r := NewResampler(inRate, outRate)

			var streamed []float32
			for off := 0; off < len(in); off += block {
				end := min(off+block, len(in))
				streamed = append(streamed, r.Process(in[off:end])...)
			}

			ref := referenceResample(in, inRate, outRate)

			if abs(len(streamed)-len(ref)) > 1 {
				t.Fatalf("count: streamed=%d ref=%d (want within 1)", len(streamed), len(ref))
			}

			n := min(len(streamed), len(ref))
			var maxErr float64
			for i := range n {
				e := math.Abs(float64(streamed[i] - ref[i]))
				if e > maxErr {
					maxErr = e
				}
			}
			if maxErr > 1e-5 {
				t.Errorf("max abs error %g exceeds tolerance", maxErr)
			}
		})
	}
}

func TestResampler_Reset(t *testing.T) {
	in := []float32{0, 1, 2, 3, 4, 5, 6, 7}
	r := NewResampler(48000, 16000)

	first := append([]float32(nil), feedAll(r, in, 3)...)
	r.Reset()
	second := append([]float32(nil), feedAll(r, in, 3)...)

	if len(first) != len(second) {
		t.Fatalf("reset should reproduce identical output: len %d vs %d", len(first), len(second))
	}
	for i := range first {
		if math.Abs(float64(first[i]-second[i])) > 1e-6 {
			t.Errorf("post-reset[%d]=%f, want %f", i, second[i], first[i])
		}
	}
}

// =============================================================================

// referenceResample mirrors what one-shot ResampleLinear computes, used as the
// ground truth for the blocking test.
func referenceResample(samples []float32, inRate, outRate int) []float32 {
	return ResampleLinear(samples, inRate, outRate)
}

func feedAll(r *Resampler, in []float32, block int) []float32 {
	var out []float32
	for off := 0; off < len(in); off += block {
		end := min(off+block, len(in))
		out = append(out, r.Process(in[off:end])...)
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func blockName(n int) string {
	switch n {
	case 1:
		return "block-1"
	case 7:
		return "block-7"
	case 160:
		return "block-160"
	case 441:
		return "block-441"
	case 1024:
		return "block-1024"
	default:
		return "block-all"
	}
}
