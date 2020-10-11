package g11n

import (
	"fmt"
	"reflect"

	g11nLocale "github.com/sgatev/g11n/locale"

	"golang.org/x/text/language"
)

// Application constants.
const (
	defaultMessageTag = "default"
)

// Error message patterns.
const (
	wrongResultsCountMessage = "Wrong number of results in a g11n message. Expected 1, got %v."
	unknownFormatMessage     = "Unknown locale format '%v'."
	unknownLocaleTag         = "Unknown locale '%v'."
)

// paramFormatter represents a type that supports custom formatting
// when it is used as parameter in a call to a g11n message.
type paramFormatter interface {

	// G11nParam formats a type in a specific way when passed to a g11n message.
	G11nParam() string
}

// resultFormatter represents a type that supports custom formatting
// when it is returned from a call to a g11n message.
type resultFormatter interface {

	// G11nResult accepts a formatted g11n message and modifies it before returning.
	G11nResult(formattedMessage string) string
}

type stringInitializer func()

// formatParam extracts the data from a reflected argument value and returns it.
func formatParam(value reflect.Value) interface{} {
	valueInterface := value.Interface()

	if paramFormatter, ok := valueInterface.(paramFormatter); ok {
		return paramFormatter.G11nParam()
	}

	return valueInterface
}

// localeInfo encapsulates the data required to parse a localization file.
type localeInfo struct {
	format string
	path   string
}

// MessageFactory initializes message structs and provides language
// translations to messages.
type MessageFactory struct {
	locales            map[language.Tag]localeInfo
	dictionary         map[string]string
	stringInitializers []stringInitializer
}

// New returns a fresh G11n message factory.
func New() *MessageFactory {
	return &MessageFactory{
		dictionary: map[string]string{},
		locales:    map[language.Tag]localeInfo{},
	}
}

// Locales returns the registered locales in a message factory.
func (mf *MessageFactory) Locales() []language.Tag {
	locales := make([]language.Tag, 0, len(mf.locales))

	for locale := range mf.locales {
		locales = append(locales, locale)
	}

	return locales
}

// SetLocale registers a locale file in the specified format.
func (mf *MessageFactory) SetLocale(tag language.Tag, format, path string) {
	mf.locales[tag] = localeInfo{
		format: format,
		path:   path,
	}
}

// SetLocales registers locale files in the specified format.
func (mf *MessageFactory) SetLocales(locales map[language.Tag]string, format string) {
	for tag, path := range locales {
		mf.SetLocale(tag, format, path)
	}
}

// LoadLocale sets the currently active locale for the messages generated
// by this factory.
func (mf *MessageFactory) LoadLocale(tag language.Tag) {
	locale, ok := mf.locales[tag]
	if !ok {
		panic(fmt.Sprintf(unknownLocaleTag, tag))
	}

	loader, ok := g11nLocale.GetLoader(locale.format)
	if !ok {
		panic(fmt.Sprintf(unknownFormatMessage, locale.format))
	}

	mf.dictionary = loader.Load(locale.path)

	for _, initializer := range mf.stringInitializers {
		initializer()
	}
}

// Init initializes the message fields of a structure pointer.
func (mf *MessageFactory) Init(structPtr interface{}) interface{} {
	mf.initializeStruct(structPtr)

	return structPtr
}

// messageHandler creates a handler that formats a message based on provided parameters.
func (mf *MessageFactory) messageHandler(messagePattern, messageKey string, resultType reflect.Type) func([]reflect.Value) []reflect.Value {
	return func(args []reflect.Value) []reflect.Value {
		// Extract localized message.
		if message, ok := mf.dictionary[messageKey]; ok {
			messagePattern = message
		}

		// Format message parameters.
		var formattedParams []interface{}
		for _, arg := range args {
			formattedParams = append(formattedParams, formatParam(arg))
		}

		// Find the result message value.
		message := fmt.Sprintf(messagePattern, formattedParams...)
		messageValue := reflect.ValueOf(message)

		// Format message result.
		resultValue := reflect.New(resultType).Elem()
		if resultFormatter, ok := resultValue.Interface().(resultFormatter); ok {
			formattedResult := resultFormatter.G11nResult(message)
			messageValue = reflect.ValueOf(formattedResult).Convert(resultType)
		}
		resultValue.Set(messageValue)

		return []reflect.Value{resultValue}
	}
}

// initializeStruct initializes the message fields of a struct pointer.
func (mf *MessageFactory) initializeStruct(structPtr interface{}) {
	instance := reflect.Indirect(reflect.ValueOf(structPtr))
	concreteType := instance.Type()

	// Initialize each message func of the struct.
	for i := 0; i < concreteType.NumField(); i++ {
		field := concreteType.Field(i)
		instanceField := instance.FieldByName(field.Name)

		if field.Anonymous {
			mf.initializeEmbeddedStruct(field, instanceField)
		} else {
			mf.initializeField(concreteType, field, instanceField)
		}
	}
}

// initializeEmbeddedStruct initializes the message fields of an embedded struct.
func (mf *MessageFactory) initializeEmbeddedStruct(
	field reflect.StructField,
	instanceField reflect.Value) {

	// Create the embedded struct.
	embeddedStruct := reflect.New(field.Type.Elem())
	instanceField.Set(embeddedStruct)

	// Initialize the messages of the embedded struct.
	mf.initializeStruct(embeddedStruct.Interface())
}

// initializeField initializes a message field.
func (mf *MessageFactory) initializeField(
	concreteType reflect.Type,
	field reflect.StructField,
	instanceField reflect.Value) {

	messageKey := fmt.Sprintf("%v.%v", concreteType.Name(), field.Name)

	// Extract default message.
	messagePattern := field.Tag.Get(defaultMessageTag)

	if field.Type.Kind() == reflect.String {
		// Initialize string field.

		message := messagePattern

		// Format message result.
		if resultFormatter, ok := instanceField.Interface().(resultFormatter); ok {
			message = resultFormatter.G11nResult(message)
		}

		mf.stringInitializers = append(mf.stringInitializers, func() {
			message := messagePattern

			// Extract localized message.
			if messagePattern, ok := mf.dictionary[messageKey]; ok {
				message = messagePattern
			}

			instanceField.SetString(message)
		})

		instanceField.SetString(message)
	} else {
		// Initialize func field.

		// Check if return type of the message func is correct.
		if field.Type.NumOut() != 1 {
			panic(fmt.Sprintf(wrongResultsCountMessage, field.Type.NumOut()))
		}

		resultType := field.Type.Out(0)

		// Create proxy function for handling the message.
		messageProxyFunc := reflect.MakeFunc(
			field.Type, mf.messageHandler(messagePattern, messageKey, resultType))

		instanceField.Set(messageProxyFunc)
	}
}
