package modelmeta_test

import (
	"testing"

	"video-canvas/internal/provider/modelmeta"
)

func TestLookupModalities(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		vision, video, found bool
	}{
		{"google/gemini-2.5-flash", true, true, true},
		{" GOOGLE/GEMINI-2.5-FLASH:free@eu ", true, true, true},
		{"gemini-2.5-flash-latest", true, true, true},
		{"gpt-4o", true, false, true},
		{"private-model-no-match", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, found := modelmeta.LookupModalities(tc.name)
			if found != tc.found || got.Vision != tc.vision || got.Video != tc.video {
				t.Fatalf("got=%+v found=%v", got, found)
			}
		})
	}
}
