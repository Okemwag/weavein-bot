package eventing

// InvalidEvent marks a provider update that must be quarantined without retrying.
type InvalidEvent string

func (e InvalidEvent) Error() string { return string(e) }

// IgnoredEvent marks expected traffic that needs no action and no operator review.
type IgnoredEvent string

func (e IgnoredEvent) Error() string { return string(e) }

// DependencyPending marks a valid update that arrived before a related event.
// The inbox retries it for a bounded period without quarantining it.
type DependencyPending string

func (e DependencyPending) Error() string { return string(e) }
