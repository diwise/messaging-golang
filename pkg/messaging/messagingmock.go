// Code generated by moq; DO NOT EDIT.
// github.com/matryer/moq

package messaging

import (
	"sync"
)

// Ensure, that ContextMock does implement Context.
// If this is not the case, regenerate this file with moq.
var _ Context = &ContextMock{}

// ContextMock is a mock implementation of Context.
//
// 	func TestSomethingThatUsesContext(t *testing.T) {
//
// 		// make and configure a mocked Context
// 		mockedContext := &ContextMock{
// 			CloseFunc: func()  {
// 				panic("mock out the Close method")
// 			},
// 			NoteToSelfFunc: func(command CommandMessage) error {
// 				panic("mock out the NoteToSelf method")
// 			},
// 			PublishOnTopicFunc: func(message TopicMessage) error {
// 				panic("mock out the PublishOnTopic method")
// 			},
// 			RegisterCommandHandlerFunc: func(contentType string, handler CommandHandler) error {
// 				panic("mock out the RegisterCommandHandler method")
// 			},
// 			RegisterTopicMessageHandlerFunc: func(routingKey string, handler TopicMessageHandler)  {
// 				panic("mock out the RegisterTopicMessageHandler method")
// 			},
// 			SendCommandToFunc: func(command CommandMessage, key string) error {
// 				panic("mock out the SendCommandTo method")
// 			},
// 			SendResponseToFunc: func(response CommandMessage, key string) error {
// 				panic("mock out the SendResponseTo method")
// 			},
// 		}
//
// 		// use mockedContext in code that requires Context
// 		// and then make assertions.
//
// 	}
type ContextMock struct {
	// CloseFunc mocks the Close method.
	CloseFunc func()

	// NoteToSelfFunc mocks the NoteToSelf method.
	NoteToSelfFunc func(command CommandMessage) error

	// PublishOnTopicFunc mocks the PublishOnTopic method.
	PublishOnTopicFunc func(message TopicMessage) error

	// RegisterCommandHandlerFunc mocks the RegisterCommandHandler method.
	RegisterCommandHandlerFunc func(contentType string, handler CommandHandler) error

	// RegisterTopicMessageHandlerFunc mocks the RegisterTopicMessageHandler method.
	RegisterTopicMessageHandlerFunc func(routingKey string, handler TopicMessageHandler)

	// SendCommandToFunc mocks the SendCommandTo method.
	SendCommandToFunc func(command CommandMessage, key string) error

	// SendResponseToFunc mocks the SendResponseTo method.
	SendResponseToFunc func(response CommandMessage, key string) error

	// calls tracks calls to the methods.
	calls struct {
		// Close holds details about calls to the Close method.
		Close []struct {
		}
		// NoteToSelf holds details about calls to the NoteToSelf method.
		NoteToSelf []struct {
			// Command is the command argument value.
			Command CommandMessage
		}
		// PublishOnTopic holds details about calls to the PublishOnTopic method.
		PublishOnTopic []struct {
			// Message is the message argument value.
			Message TopicMessage
		}
		// RegisterCommandHandler holds details about calls to the RegisterCommandHandler method.
		RegisterCommandHandler []struct {
			// ContentType is the contentType argument value.
			ContentType string
			// Handler is the handler argument value.
			Handler CommandHandler
		}
		// RegisterTopicMessageHandler holds details about calls to the RegisterTopicMessageHandler method.
		RegisterTopicMessageHandler []struct {
			// RoutingKey is the routingKey argument value.
			RoutingKey string
			// Handler is the handler argument value.
			Handler TopicMessageHandler
		}
		// SendCommandTo holds details about calls to the SendCommandTo method.
		SendCommandTo []struct {
			// Command is the command argument value.
			Command CommandMessage
			// Key is the key argument value.
			Key string
		}
		// SendResponseTo holds details about calls to the SendResponseTo method.
		SendResponseTo []struct {
			// Response is the response argument value.
			Response CommandMessage
			// Key is the key argument value.
			Key string
		}
	}
	lockClose                       sync.RWMutex
	lockNoteToSelf                  sync.RWMutex
	lockPublishOnTopic              sync.RWMutex
	lockRegisterCommandHandler      sync.RWMutex
	lockRegisterTopicMessageHandler sync.RWMutex
	lockSendCommandTo               sync.RWMutex
	lockSendResponseTo              sync.RWMutex
}

// Close calls CloseFunc.
func (mock *ContextMock) Close() {
	callInfo := struct {
	}{}
	mock.lockClose.Lock()
	mock.calls.Close = append(mock.calls.Close, callInfo)
	mock.lockClose.Unlock()
	if mock.CloseFunc == nil {
		return
	}
	mock.CloseFunc()
}

// CloseCalls gets all the calls that were made to Close.
// Check the length with:
//     len(mockedContext.CloseCalls())
func (mock *ContextMock) CloseCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockClose.RLock()
	calls = mock.calls.Close
	mock.lockClose.RUnlock()
	return calls
}

// NoteToSelf calls NoteToSelfFunc.
func (mock *ContextMock) NoteToSelf(command CommandMessage) error {
	callInfo := struct {
		Command CommandMessage
	}{
		Command: command,
	}
	mock.lockNoteToSelf.Lock()
	mock.calls.NoteToSelf = append(mock.calls.NoteToSelf, callInfo)
	mock.lockNoteToSelf.Unlock()
	if mock.NoteToSelfFunc == nil {
		var (
			errOut error
		)
		return errOut
	}
	return mock.NoteToSelfFunc(command)
}

// NoteToSelfCalls gets all the calls that were made to NoteToSelf.
// Check the length with:
//     len(mockedContext.NoteToSelfCalls())
func (mock *ContextMock) NoteToSelfCalls() []struct {
	Command CommandMessage
} {
	var calls []struct {
		Command CommandMessage
	}
	mock.lockNoteToSelf.RLock()
	calls = mock.calls.NoteToSelf
	mock.lockNoteToSelf.RUnlock()
	return calls
}

// PublishOnTopic calls PublishOnTopicFunc.
func (mock *ContextMock) PublishOnTopic(message TopicMessage) error {
	callInfo := struct {
		Message TopicMessage
	}{
		Message: message,
	}
	mock.lockPublishOnTopic.Lock()
	mock.calls.PublishOnTopic = append(mock.calls.PublishOnTopic, callInfo)
	mock.lockPublishOnTopic.Unlock()
	if mock.PublishOnTopicFunc == nil {
		var (
			errOut error
		)
		return errOut
	}
	return mock.PublishOnTopicFunc(message)
}

// PublishOnTopicCalls gets all the calls that were made to PublishOnTopic.
// Check the length with:
//     len(mockedContext.PublishOnTopicCalls())
func (mock *ContextMock) PublishOnTopicCalls() []struct {
	Message TopicMessage
} {
	var calls []struct {
		Message TopicMessage
	}
	mock.lockPublishOnTopic.RLock()
	calls = mock.calls.PublishOnTopic
	mock.lockPublishOnTopic.RUnlock()
	return calls
}

// RegisterCommandHandler calls RegisterCommandHandlerFunc.
func (mock *ContextMock) RegisterCommandHandler(contentType string, handler CommandHandler) error {
	callInfo := struct {
		ContentType string
		Handler     CommandHandler
	}{
		ContentType: contentType,
		Handler:     handler,
	}
	mock.lockRegisterCommandHandler.Lock()
	mock.calls.RegisterCommandHandler = append(mock.calls.RegisterCommandHandler, callInfo)
	mock.lockRegisterCommandHandler.Unlock()
	if mock.RegisterCommandHandlerFunc == nil {
		var (
			errOut error
		)
		return errOut
	}
	return mock.RegisterCommandHandlerFunc(contentType, handler)
}

// RegisterCommandHandlerCalls gets all the calls that were made to RegisterCommandHandler.
// Check the length with:
//     len(mockedContext.RegisterCommandHandlerCalls())
func (mock *ContextMock) RegisterCommandHandlerCalls() []struct {
	ContentType string
	Handler     CommandHandler
} {
	var calls []struct {
		ContentType string
		Handler     CommandHandler
	}
	mock.lockRegisterCommandHandler.RLock()
	calls = mock.calls.RegisterCommandHandler
	mock.lockRegisterCommandHandler.RUnlock()
	return calls
}

// RegisterTopicMessageHandler calls RegisterTopicMessageHandlerFunc.
func (mock *ContextMock) RegisterTopicMessageHandler(routingKey string, handler TopicMessageHandler) {
	callInfo := struct {
		RoutingKey string
		Handler    TopicMessageHandler
	}{
		RoutingKey: routingKey,
		Handler:    handler,
	}
	mock.lockRegisterTopicMessageHandler.Lock()
	mock.calls.RegisterTopicMessageHandler = append(mock.calls.RegisterTopicMessageHandler, callInfo)
	mock.lockRegisterTopicMessageHandler.Unlock()
	if mock.RegisterTopicMessageHandlerFunc == nil {
		return
	}
	mock.RegisterTopicMessageHandlerFunc(routingKey, handler)
}

// RegisterTopicMessageHandlerCalls gets all the calls that were made to RegisterTopicMessageHandler.
// Check the length with:
//     len(mockedContext.RegisterTopicMessageHandlerCalls())
func (mock *ContextMock) RegisterTopicMessageHandlerCalls() []struct {
	RoutingKey string
	Handler    TopicMessageHandler
} {
	var calls []struct {
		RoutingKey string
		Handler    TopicMessageHandler
	}
	mock.lockRegisterTopicMessageHandler.RLock()
	calls = mock.calls.RegisterTopicMessageHandler
	mock.lockRegisterTopicMessageHandler.RUnlock()
	return calls
}

// SendCommandTo calls SendCommandToFunc.
func (mock *ContextMock) SendCommandTo(command CommandMessage, key string) error {
	callInfo := struct {
		Command CommandMessage
		Key     string
	}{
		Command: command,
		Key:     key,
	}
	mock.lockSendCommandTo.Lock()
	mock.calls.SendCommandTo = append(mock.calls.SendCommandTo, callInfo)
	mock.lockSendCommandTo.Unlock()
	if mock.SendCommandToFunc == nil {
		var (
			errOut error
		)
		return errOut
	}
	return mock.SendCommandToFunc(command, key)
}

// SendCommandToCalls gets all the calls that were made to SendCommandTo.
// Check the length with:
//     len(mockedContext.SendCommandToCalls())
func (mock *ContextMock) SendCommandToCalls() []struct {
	Command CommandMessage
	Key     string
} {
	var calls []struct {
		Command CommandMessage
		Key     string
	}
	mock.lockSendCommandTo.RLock()
	calls = mock.calls.SendCommandTo
	mock.lockSendCommandTo.RUnlock()
	return calls
}

// SendResponseTo calls SendResponseToFunc.
func (mock *ContextMock) SendResponseTo(response CommandMessage, key string) error {
	callInfo := struct {
		Response CommandMessage
		Key      string
	}{
		Response: response,
		Key:      key,
	}
	mock.lockSendResponseTo.Lock()
	mock.calls.SendResponseTo = append(mock.calls.SendResponseTo, callInfo)
	mock.lockSendResponseTo.Unlock()
	if mock.SendResponseToFunc == nil {
		var (
			errOut error
		)
		return errOut
	}
	return mock.SendResponseToFunc(response, key)
}

// SendResponseToCalls gets all the calls that were made to SendResponseTo.
// Check the length with:
//     len(mockedContext.SendResponseToCalls())
func (mock *ContextMock) SendResponseToCalls() []struct {
	Response CommandMessage
	Key      string
} {
	var calls []struct {
		Response CommandMessage
		Key      string
	}
	mock.lockSendResponseTo.RLock()
	calls = mock.calls.SendResponseTo
	mock.lockSendResponseTo.RUnlock()
	return calls
}
