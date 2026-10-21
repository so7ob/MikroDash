package subscriber

import "errors"

// admissionDenied keeps transaction-time policy evidence separate from the
// public error string. Existing errors.Is callers retain ErrDenied semantics.
type admissionDenied struct{ reasons []string }

func (e *admissionDenied) Error() string { return ErrDenied.Error() }
func (e *admissionDenied) Unwrap() error { return ErrDenied }

func denyAdmission(reasons ...string) error {
	if len(reasons) == 0 {
		reasons = []string{"authorization-denied"}
	}
	return &admissionDenied{reasons: append([]string{}, reasons...)}
}

func admissionReasons(err error) []string {
	var denied *admissionDenied
	if errors.As(err, &denied) {
		return append([]string{}, denied.reasons...)
	}
	if errors.Is(err, ErrDenied) {
		return []string{"authorization-denied"}
	}
	return []string{"authorization-unavailable"}
}
