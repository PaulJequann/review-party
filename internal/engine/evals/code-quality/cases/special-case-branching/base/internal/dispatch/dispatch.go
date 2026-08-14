package dispatch

type Event struct {
	Kind    string
	Payload []byte
}

type Handler func(Event) error

func Decode(payload []byte) (Event, error) {
	return Event{Kind: string(payload[:1]), Payload: payload[1:]}, nil
}

func Dispatch(payload []byte, handlers map[string]Handler) error {
	event, err := Decode(payload)
	if err != nil {
		return err
	}
	handler, ok := handlers[event.Kind]
	if !ok {
		return ErrUnknownEvent
	}
	return handler(event)
}
