package zap

import (
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrAddObjNotImplemented = errors.New("AddObject is not implemented for logfmt encoder. Please consider implementing the LogMarshaler interface.")

var logfmtPool = sync.Pool{
	New: func() interface{} {
		return &logfmtEncoder{
			bytes: make([]byte, 0, _initialBufSize),
		}
	},
}

type logfmtEncoder struct {
	bytes []byte
}

func (enc *logfmtEncoder) truncate() {
	enc.bytes = enc.bytes[:0]
}

func newLogfmtEncoder() *logfmtEncoder {
	enc := logfmtPool.Get().(*logfmtEncoder)
	enc.truncate()
	return enc
}

// Clone copies the current encoder, including any data already encoded
func (enc *logfmtEncoder) Clone() Encoder {
	clone := logfmtPool.Get().(*logfmtEncoder)
	clone.truncate()
	clone.bytes = append(clone.bytes, enc.bytes...)
	return clone
}

func (enc *logfmtEncoder) Free() {
	logfmtPool.Put(enc)
}

// WriteEntry writes a complete log message to the supplied writer, including
// the encoder's accumulated fields. It doesn't modify or lock the encoder's
// underlying byte slice. It's safe to call from multiple goroutines, but it's
// not safe to call WriteEntry while adding fields.
func (enc *logfmtEncoder) WriteEntry(sink io.Writer, msg string, lvl Level, t time.Time) error {
	final := logfmtPool.Get().(*logfmtEncoder)
	final.truncate()
	final.addLevel(lvl)
	final.AddString("msg", msg)
	final.addTime(t)
	final.AddString("ts", t.Format(time.RFC3339))
	if len(enc.bytes) > 0 {
		final.spacing()
		final.bytes = append(final.bytes, enc.bytes...)
	}
	final.bytes = append(final.bytes, '\n')

	expectedBytes := len(final.bytes)
	n, err := sink.Write(final.bytes)
	final.Free()
	if err != nil {
		return err
	}
	if n != expectedBytes {
		return fmt.Errorf("incomplete write: only wrote %v of %v bytes", n, expectedBytes)
	}
	return nil
}

// https://github.com/kr/logfmt/blob/master/decode.go#L11-L20
//
// EBNFish:
//
// 	ident_byte = any byte greater than ' ', excluding '=' and '"'
// 	string_byte = any byte excluding '"' and '\'
// 	garbage = !ident_byte
// 	ident = ident_byte, { ident byte }
// 	key = ident
// 	value = ident | '"', { string_byte | '\', '"' }, '"'
// 	pair = key, '=', value | key, '=' | key
// 	message = { garbage, pair }, garbage

// addKey adds the key to the bytes array.
// It ensures that the key conforms to the ident specification, replacing offending characters with the unicode replacement character (\ufffd)
func (enc *logfmtEncoder) addKey(key string) {
	for _, kchar := range key {
		if kchar > ' ' && kchar != '=' && kchar != '"' { // ident_byte
			enc.bytes = append(enc.bytes, byte(kchar))
		} else { // garbage
			enc.bytes = append(enc.bytes, `\ufffd`...)
		}
	}
}

// spacing adds a garbage byte
func (enc *logfmtEncoder) spacing() {
	if len(enc.bytes) == 0 {
		return
	}
	enc.bytes = append(enc.bytes, ' ')
}

// AddBool appends the key and, if the value is false, the value 'false' (lone keys are interpreted as 'true'.)
func (enc *logfmtEncoder) AddBool(key string, value bool) {
	enc.spacing()
	enc.addKey(key)
	if value == false {
		enc.bytes = append(enc.bytes, `=false`...)
	}
}

// AddFloat64 appends the key and float64 value to the encoder's log line.
func (enc *logfmtEncoder) AddFloat64(key string, value float64) {
	enc.spacing()
	enc.addKey(key)
	enc.bytes = append(enc.bytes, '=')
	switch {
	case math.IsNaN(value):
		enc.bytes = append(enc.bytes, `NaN`...)
	case math.IsInf(value, 1):
		enc.bytes = append(enc.bytes, `+Inf`...)
	case math.IsInf(value, -1):
		enc.bytes = append(enc.bytes, `-Inf`...)
	default:
		enc.bytes = strconv.AppendFloat(enc.bytes, value, 'f', -1, 64)
	}
}

// AddInt appends the key and int value to the encoder's log line.
func (enc *logfmtEncoder) AddInt(key string, value int) {
	enc.AddInt64(key, int64(value))
}

// AddInt64 appends the key and int64 value to the encoder's log line.
func (enc *logfmtEncoder) AddInt64(key string, value int64) {
	enc.spacing()
	enc.addKey(key)
	enc.bytes = append(enc.bytes, '=')
	enc.bytes = strconv.AppendInt(enc.bytes, value, 10)
}

// AddUInt appends the key and uint value to the encoder's log line.
func (enc *logfmtEncoder) AddUint(key string, value uint) {
	enc.AddUint64(key, uint64(value))
}

// AddUInt64 appends the key and uint64 value to the encoder's log line.
func (enc *logfmtEncoder) AddUint64(key string, value uint64) {
	enc.spacing()
	enc.addKey(key)
	enc.bytes = append(enc.bytes, '=')
	enc.bytes = strconv.AppendUint(enc.bytes, value, 10)
}

func (enc *logfmtEncoder) AddUintptr(key string, val uintptr) {
	enc.spacing()
	enc.addKey(key)
	enc.bytes = append(enc.bytes, `=0x`...)
	enc.bytes = strconv.AppendUint(enc.bytes, uint64(val), 16)
}

// AddMarshaler adds a LogMarshaler to the encoder's fields. Since there's no real nesting in logfmt, we'll just trust the LogMarshaler to "do the right thing".
func (enc *logfmtEncoder) AddMarshaler(key string, obj LogMarshaler) error {
	return obj.MarshalLog(enc)
}

// AddObject is not implemented for LogfmtEncoder
func (enc *logfmtEncoder) AddObject(key string, value interface{}) error {
	enc.AddString(key, fmt.Sprintf("%+v", value))
	return nil
}

// AddString adds a key and string value to the encoder's logline, escaping any '\' or '"' runes
func (enc *logfmtEncoder) AddString(key, value string) {
	enc.spacing()
	enc.addKey(key)
	enc.bytes = append(enc.bytes, '=', '"')

	for i := 0; i < len(value); {
		if b := value[i]; b < utf8.RuneSelf {
			i++

			switch b {
			case '\\', '"':
				enc.bytes = append(enc.bytes, '\\', b)
			case '\n':
				enc.bytes = append(enc.bytes, '\\', 'n')
			case '\r':
				enc.bytes = append(enc.bytes, '\\', 'r')
			case '\t':
				enc.bytes = append(enc.bytes, '\\', 't')
			default:
				enc.bytes = append(enc.bytes, b)
			}

			continue
		}
		c, size := utf8.DecodeRuneInString(value[i:])
		if c == utf8.RuneError && size == 1 {
			enc.bytes = append(enc.bytes, `\ufffd`...)
			i++
			continue
		}
		enc.bytes = append(enc.bytes, value[i:i+size]...)
		i += size
	}

	enc.bytes = append(enc.bytes, '"')
}

func (enc *logfmtEncoder) addLevel(lvl Level) {
	enc.spacing()
	enc.addKey("level")
	enc.bytes = append(enc.bytes, '=')
	switch lvl {
	case DebugLevel:
		enc.bytes = append(enc.bytes, `debug`...)
	case InfoLevel:
		enc.bytes = append(enc.bytes, `info`...)
	case WarnLevel:
		enc.bytes = append(enc.bytes, `warn`...)
	case ErrorLevel:
		enc.bytes = append(enc.bytes, `error`...)
	case PanicLevel:
		enc.bytes = append(enc.bytes, `panic`...)
	case FatalLevel:
		enc.bytes = append(enc.bytes, `fatal`...)
	default:
		enc.bytes = strconv.AppendInt(enc.bytes, int64(lvl), 10)
	}
}

func (enc *logfmtEncoder) addTime(ts time.Time) {
	enc.spacing()
	enc.addKey("ts")
	enc.bytes = append(enc.bytes, '=')
	enc.bytes = ts.AppendFormat(enc.bytes, time.RFC3339)
}
