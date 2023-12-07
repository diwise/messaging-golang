package messaging

import "encoding/json"

type msg struct {
	contentType string
	body        []byte
}

func (m *msg) Body() []byte {
	return m.body
}

func (m *msg) ContentType() string {
	return m.contentType
}

func NewMessageJSON(contentType string, body any) (Message, error) {

	m := &msg{
		contentType: contentType,
	}

	var err error
	m.body, err = json.Marshal(body)
	if err != nil {
		return nil, err
	}

	return m, nil
}

type topicmsg struct {
	m         Message
	topicName string
}

func (tm *topicmsg) Body() []byte {
	return tm.m.Body()
}

func (tm *topicmsg) ContentType() string {
	return tm.m.ContentType()
}

func (tm *topicmsg) TopicName() string {
	return tm.topicName
}

func NewTopicMessageJSON(topic, contentType string, body any) (TopicMessage, error) {

	msgJson, err := NewMessageJSON(contentType, body)
	if err != nil {
		return nil, err
	}

	tm := &topicmsg{
		m:         msgJson,
		topicName: topic,
	}

	return tm, nil
}
