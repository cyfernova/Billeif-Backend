package turn

import (
	"reflect"
	"testing"

	voicesession "invoice-backend/internal/voice/session"
)

func TestSupportedResponseLanguagesMatchSessionContractAndAreDefensive(t *testing.T) {
	want := []string{
		"bn-IN", "en-IN", "gu-IN", "hi-IN", "kn-IN", "ml-IN",
		"mr-IN", "od-IN", "pa-IN", "ta-IN", "te-IN",
	}
	first := SupportedResponseLanguages()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("SupportedResponseLanguages() = %v, want %v", first, want)
	}
	if !reflect.DeepEqual(first, voicesession.SpokenLanguages) {
		t.Fatalf("turn languages = %v, session languages = %v", first, voicesession.SpokenLanguages)
	}
	first[0] = "mutated"
	if second := SupportedResponseLanguages(); !reflect.DeepEqual(second, want) {
		t.Fatalf("supported languages share mutable backing storage: %v", second)
	}
}

func TestSelectResponseLanguageUsesCanonicalDetectionOrFallback(t *testing.T) {
	tests := []struct {
		name     string
		detected string
		fallback string
		want     LanguageSelection
	}{
		{
			name: "supported detection wins", detected: "hi-IN", fallback: "en-IN",
			want: LanguageSelection{ProviderLanguage: "hi-IN", DetectedLanguage: "hi-IN", ResponseLanguage: "hi-IN"},
		},
		{
			name: "legacy Odia spelling is normalized", detected: "or-IN", fallback: "en-IN",
			want: LanguageSelection{ProviderLanguage: "or-IN", DetectedLanguage: "od-IN", ResponseLanguage: "od-IN"},
		},
		{
			name: "unsupported detection uses configured fallback", detected: "fr-FR", fallback: "mr-IN",
			want: LanguageSelection{ProviderLanguage: "fr-FR", ResponseLanguage: "mr-IN", UsedFallback: true},
		},
		{
			name: "legacy Odia fallback is normalized", detected: "fr-FR", fallback: "or-IN",
			want: LanguageSelection{ProviderLanguage: "fr-FR", ResponseLanguage: "od-IN", UsedFallback: true},
		},
		{
			name: "missing detection uses configured fallback", detected: "", fallback: "ta-IN",
			want: LanguageSelection{ResponseLanguage: "ta-IN", UsedFallback: true},
		},
		{
			name: "invalid fallback resolves to English", detected: "fr-FR", fallback: "de-DE",
			want: LanguageSelection{ProviderLanguage: "fr-FR", ResponseLanguage: "en-IN", UsedFallback: true},
		},
		{
			name: "case variants are not silently accepted", detected: "HI-in", fallback: "kn-IN",
			want: LanguageSelection{ProviderLanguage: "HI-in", ResponseLanguage: "kn-IN", UsedFallback: true},
		},
		{
			name: "surrounding whitespace is not silently accepted", detected: " hi-IN ", fallback: "gu-IN",
			want: LanguageSelection{ProviderLanguage: " hi-IN ", ResponseLanguage: "gu-IN", UsedFallback: true},
		},
		{
			name: "unsafe provider value is not retained", detected: "hi-IN\nsecret", fallback: "pa-IN",
			want: LanguageSelection{ResponseLanguage: "pa-IN", UsedFallback: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SelectResponseLanguage(test.detected, test.fallback); got != test.want {
				t.Fatalf("SelectResponseLanguage(%q, %q) = %#v, want %#v", test.detected, test.fallback, got, test.want)
			}
		})
	}
}

func TestSelectResponseLanguageRejectsOversizedProviderValue(t *testing.T) {
	got := SelectResponseLanguage("this-language-code-is-far-too-long", "te-IN")
	want := LanguageSelection{ResponseLanguage: "te-IN", UsedFallback: true}
	if got != want {
		t.Fatalf("oversized provider language selection = %#v, want %#v", got, want)
	}
}
