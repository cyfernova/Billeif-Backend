package turn

const (
	defaultResponseLanguage  = "en-IN"
	maxProviderLanguageBytes = 16
)

var responseLanguages = [...]string{
	"bn-IN", "en-IN", "gu-IN", "hi-IN", "kn-IN", "ml-IN",
	"mr-IN", "od-IN", "pa-IN", "ta-IN", "te-IN",
}

type LanguageSelection struct {
	// ProviderLanguage is the safe, original value returned by the provider.
	// It remains distinct from DetectedLanguage so the legacy or-IN spelling
	// can be observed without leaking into the canonical application contract.
	ProviderLanguage string
	DetectedLanguage string
	ResponseLanguage string
	UsedFallback     bool
}

func SupportedResponseLanguages() []string {
	languages := make([]string, len(responseLanguages))
	copy(languages, responseLanguages[:])
	return languages
}

func SelectResponseLanguage(detected, fallback string) LanguageSelection {
	selection := LanguageSelection{ProviderLanguage: safeProviderLanguage(detected)}
	canonicalDetected := canonicalResponseLanguage(selection.ProviderLanguage)
	if supportedResponseLanguage(canonicalDetected) {
		selection.DetectedLanguage = canonicalDetected
		selection.ResponseLanguage = canonicalDetected
		return selection
	}

	selection.UsedFallback = true
	canonicalFallback := canonicalResponseLanguage(fallback)
	if supportedResponseLanguage(canonicalFallback) {
		selection.ResponseLanguage = canonicalFallback
	} else {
		selection.ResponseLanguage = defaultResponseLanguage
	}
	return selection
}

func canonicalResponseLanguage(value string) string {
	if value == "or-IN" {
		return "od-IN"
	}
	return value
}

func safeProviderLanguage(value string) string {
	if len(value) == 0 || len(value) > maxProviderLanguageBytes {
		return ""
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return ""
		}
	}
	return value
}

func supportedResponseLanguage(value string) bool {
	for _, supported := range responseLanguages {
		if value == supported {
			return true
		}
	}
	return false
}
