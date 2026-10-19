package schema

func CloneMessages(messages []*Message) []*Message {
	if messages == nil {
		return nil
	}
	result := make([]*Message, len(messages))
	for index, message := range messages {
		result[index] = message.Clone()
	}
	return result
}
