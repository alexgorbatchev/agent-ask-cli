package request

// A process tree is owned before native execution begins; the platform owner
// also handles descendants that keep stdout open after their parent exits.
type processOwner struct {
	afterStart func() error
	kill       func() error
	close      func() error
}
