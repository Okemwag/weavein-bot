package eventing

// InvalidEvent marks a provider update that must be quarantined without retrying.
type InvalidEvent string

func (e InvalidEvent) Error() string { return string(e) }
