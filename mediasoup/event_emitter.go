package mediasoup

import (
	"reflect"
	"runtime/debug"
	"sync"

	"github.com/sirupsen/logrus"
)

type EventEmitter interface {
	AddListener(evt string, listeners ...interface{})
	Once(evt string, listener interface{})
	Emit(evt string, argv ...interface{}) (err error)
	SafeEmit(evt string, argv ...interface{})
	RemoveListener(evt string, listener interface{}) (ok bool)
	RemoveAllListeners(evt string)
	On(evt string, listener ...interface{})
	Off(evt string, listener interface{})
	ListenerCount(evt string) int
	Len() int
}

type (
	intervalListener struct {
		FuncValue reflect.Value
		ArgTypes  []reflect.Type
		Once      bool
	}

	eventEmitter struct {
		logger       logrus.FieldLogger
		evtListeners map[string][]*intervalListener
		mu           sync.Mutex
	}
)

func NewEventEmitter(logger logrus.FieldLogger) EventEmitter {
	return &eventEmitter{
		logger: logger,
	}
}

func (e *eventEmitter) AddListener(evt string, listeners ...interface{}) {
	if len(listeners) == 0 {
		return
	}

	var listenerValues []*intervalListener

	for _, listener := range listeners {
		listenerValue := reflect.ValueOf(listener)
		listenerType := listenerValue.Type()

		if listenerType.Kind() != reflect.Func {
			continue
		}
		var argTypes []reflect.Type

		for i := 0; i < listenerType.NumIn(); i++ {
			argTypes = append(argTypes, listenerType.In(i))
		}

		listenerValues = append(listenerValues, &intervalListener{
			FuncValue: listenerValue,
			ArgTypes:  argTypes,
		})
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.evtListeners == nil {
		e.evtListeners = make(map[string][]*intervalListener)
	}

	e.evtListeners[evt] = append(e.evtListeners[evt], listenerValues...)
}

func (e *eventEmitter) Once(evt string, listener interface{}) {
	e.AddListener(evt, listener)

	e.mu.Lock()
	defer e.mu.Unlock()

	listenerPointer := reflect.ValueOf(listener).Pointer()
	listeners := e.evtListeners[evt]

	for i := len(listeners) - 1; i >= 0; i-- {
		item := listeners[i]

		if item.FuncValue.Pointer() == listenerPointer {
			item.Once = true
			break
		}
	}
}

// Emit fires a particular event
func (e *eventEmitter) Emit(evt string, argv ...interface{}) (err error) {
	e.mu.Lock()

	if e.evtListeners == nil {
		e.mu.Unlock()
		return // has no listeners to emit yet
	}

	listeners := e.evtListeners[evt][:]

	e.mu.Unlock()

	var callArgs []reflect.Value

	for _, a := range argv {
		callArgs = append(callArgs, reflect.ValueOf(a))
	}

	for _, listener := range listeners {
		var actualCallArgs []reflect.Value

		// delete unwanted arguments
		if argc := len(listener.ArgTypes); len(callArgs) >= argc {
			actualCallArgs = callArgs[0:argc]
		} else {
			actualCallArgs = callArgs[:]
			isVariadic := listener.FuncValue.Type().IsVariadic()

			// append missing arguments with zero value
			for i, a := range listener.ArgTypes[len(callArgs):] {
				// ignore the last variadic argument
				if isVariadic && len(callArgs)+i == argc-1 {
					break
				}
				actualCallArgs = append(actualCallArgs, reflect.Zero(a))
			}
		}

		listener.FuncValue.Call(actualCallArgs)

		if listener.Once {
			e.RemoveListener(evt, listener)
		}
	}

	return
}

// SafaEmit fires a particular event and ignore panic.
func (e *eventEmitter) SafeEmit(evt string, argv ...interface{}) {
	defer func() {
		if r := recover(); r != nil {
			if logger, ok := e.logger.(*logrus.Logger); ok &&
				logger.IsLevelEnabled(logrus.DebugLevel) {
				debug.PrintStack()
			}
			e.logger.WithField("event", evt).Errorln(r)
		}
	}()

	e.Emit(evt, argv...)
}

func (e *eventEmitter) RemoveListener(evt string, listener interface{}) (ok bool) {
	if e.evtListeners == nil {
		return
	}

	if listener == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	idx := -1
	listenerPointer := reflect.ValueOf(listener).Pointer()
	listeners := e.evtListeners[evt]

	for index, item := range listeners {
		if listener == item ||
			item.FuncValue.Pointer() == listenerPointer {
			idx = index
			break
		}
	}

	if idx < 0 {
		return
	}

	var modifiedListeners []*intervalListener

	if len(listeners) > 1 {
		modifiedListeners = append(listeners[:idx], listeners[idx+1:]...)
	}

	e.evtListeners[evt] = modifiedListeners

	return true
}

func (e *eventEmitter) RemoveAllListeners(evt string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(e.evtListeners, evt)
}

func (e *eventEmitter) On(evt string, listener ...interface{}) {
	e.AddListener(evt, listener...)
}

func (e *eventEmitter) Off(evt string, listener interface{}) {
	e.RemoveListener(evt, listener)
}

func (e *eventEmitter) ListenerCount(evt string) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.evtListeners == nil {
		return 0
	}

	return len(e.evtListeners[evt])
}

func (e *eventEmitter) Len() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return len(e.evtListeners)
}
