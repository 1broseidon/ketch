---
url: https://pkg.go.dev/go.uber.org/zap
title: zap
words: 12953
---
# zap

package module

[v1.28.0](https://pkg.go.dev/go.uber.org/zap?tab=versions) Latest Latest [Go to latest](https://pkg.go.dev/go.uber.org/zap) Published: Apr 28, 2026 License: [MIT](https://pkg.go.dev/go.uber.org/zap?tab=licenses) [Imports: 23](https://pkg.go.dev/go.uber.org/zap?tab=imports) [Imported by: 119,696](https://pkg.go.dev/go.uber.org/zap?tab=importedby)

## Details

- [Learn more about best practices](https://pkg.go.dev/about#best-practices)

## Repository

[github.com/uber-go/zap](https://github.com/uber-go/zap "https://github.com/uber-go/zap")

## Links

- [![Open Source Insights Logo](https://pkg.go.dev/static/shared/icon/depsdev-logo.svg) Open Source Insights](https://deps.dev/go/go.uber.org%2Fzap/v1.28.0 "View this module on Open Source Insights")

## ![](https://pkg.go.dev/static/shared/icon/chrome_reader_mode_gm_grey_24dp.svg) README

### ⚡ zap

Blazing fast, structured, leveled logging in Go.

![Zap logo](https://github.com/uber-go/zap/raw/v1.28.0/assets/logo.png)

[![GoDoc](https://pkg.go.dev/badge/go.uber.org/zap)](https://pkg.go.dev/go.uber.org/zap) [![Build Status](https://github.com/uber-go/zap/actions/workflows/go.yml/badge.svg)](https://github.com/uber-go/zap/actions/workflows/go.yml) [![Coverage Status](https://codecov.io/gh/uber-go/zap/branch/master/graph/badge.svg)](https://codecov.io/gh/uber-go/zap)

#### Installation

`go get -u go.uber.org/zap`

Note that zap only supports the two most recent minor versions of Go.

#### Quick Start

In contexts where performance is nice, but not critical, use the `SugaredLogger`. It's 4-10x faster than other structured logging packages and includes both structured and `printf`-style APIs.

```
logger, _ := zap.NewProduction()
defer logger.Sync() // flushes buffer, if any
sugar := logger.Sugar()
sugar.Infow("failed to fetch URL",
  // Structured context as loosely typed key-value pairs.
  "url", url,
  "attempt", 3,
  "backoff", time.Second,
)
sugar.Infof("Failed to fetch URL: %s", url)
```
When performance and type safety are critical, use the `Logger`. It's even faster than the `SugaredLogger` and allocates far less, but it only supports structured logging.

```
logger, _ := zap.NewProduction()
defer logger.Sync()
logger.Info("failed to fetch URL",
  // Structured context as strongly typed Field values.
  zap.String("url", url),
  zap.Int("attempt", 3),
  zap.Duration("backoff", time.Second),
)
```
See the [documentation](https://pkg.go.dev/go.uber.org/zap) and [FAQ](https://github.com/uber-go/zap/blob/v1.28.0/FAQ.md) for more details.

#### Performance

For applications that log in the hot path, reflection-based serialization and string formatting are prohibitively expensive — they're CPU-intensive and make many small allocations. Put differently, using `encoding/json` and `fmt.Fprintf` to log tons of `interface{}`s makes your application slow.

Zap takes a different approach. It includes a reflection-free, zero-allocation JSON encoder, and the base `Logger` strives to avoid serialization overhead and allocations wherever possible. By building the high-level `SugaredLogger` on that foundation, zap lets users *choose* when they need to count every allocation and when they'd prefer a more familiar, loosely typed API.

As measured by its own [benchmarking suite](https://github.com/uber-go/zap/tree/master/benchmarks), not only is zap more performant than comparable structured logging packages — it's also faster than the standard library. Like all benchmarks, take these with a grain of salt.[1](https://pkg.go.dev/go.uber.org/zap#readme-footnote-versions)

Log a message and 10 fields:

|Package|Time|Time % to zap|Objects Allocated|
|:--|:-:|:-:|:-:|
|⚡ zap|656 ns/op|+0%|5 allocs/op|
|⚡ zap (sugared)|935 ns/op|+43%|10 allocs/op|
|zerolog|380 ns/op|-42%|1 allocs/op|
|go-kit|2249 ns/op|+243%|57 allocs/op|
|slog (LogAttrs)|2479 ns/op|+278%|40 allocs/op|
|slog|2481 ns/op|+278%|42 allocs/op|
|apex/log|9591 ns/op|+1362%|63 allocs/op|
|log15|11393 ns/op|+1637%|75 allocs/op|
|logrus|11654 ns/op|+1677%|79 allocs/op|

Log a message with a logger that already has 10 fields of context:

|Package|Time|Time % to zap|Objects Allocated|
|:--|:-:|:-:|:-:|
|⚡ zap|67 ns/op|+0%|0 allocs/op|
|⚡ zap (sugared)|84 ns/op|+25%|1 allocs/op|
|zerolog|35 ns/op|-48%|0 allocs/op|
|slog|193 ns/op|+188%|0 allocs/op|
|slog (LogAttrs)|200 ns/op|+199%|0 allocs/op|
|go-kit|2460 ns/op|+3572%|56 allocs/op|
|log15|9038 ns/op|+13390%|70 allocs/op|
|apex/log|9068 ns/op|+13434%|53 allocs/op|
|logrus|10521 ns/op|+15603%|68 allocs/op|

Log a static string, without any context or `printf`-style templating:

|Package|Time|Time % to zap|Objects Allocated|
|:--|:-:|:-:|:-:|
|⚡ zap|63 ns/op|+0%|0 allocs/op|
|⚡ zap (sugared)|81 ns/op|+29%|1 allocs/op|
|zerolog|32 ns/op|-49%|0 allocs/op|
|standard library|124 ns/op|+97%|1 allocs/op|
|slog|196 ns/op|+211%|0 allocs/op|
|slog (LogAttrs)|200 ns/op|+217%|0 allocs/op|
|go-kit|213 ns/op|+238%|9 allocs/op|
|apex/log|771 ns/op|+1124%|5 allocs/op|
|logrus|1439 ns/op|+2184%|23 allocs/op|
|log15|2069 ns/op|+3184%|20 allocs/op|

#### Development Status: Stable

All APIs are finalized, and no breaking changes will be made in the 1.x series of releases. Users of semver-aware dependency management systems should pin zap to `^1`.

#### Contributing

We encourage and support an active, healthy community of contributors — including you! Details are in the [contribution guide](https://github.com/uber-go/zap/blob/v1.28.0/CONTRIBUTING.md) and the [code of conduct](https://github.com/uber-go/zap/blob/v1.28.0/CODE_OF_CONDUCT.md). The zap maintainers keep an eye on issues and pull requests, but you can also report any negative conduct to [oss-conduct@uber.com](mailto:oss-conduct@uber.com). That email list is a private, safe space; even the zap maintainers don't have access, so don't hesitate to hold us to a high standard.

* * *

Released under the [MIT License](https://github.com/uber-go/zap/blob/v1.28.0/LICENSE).

1 In particular, keep in mind that we may be benchmarking against slightly older versions of other packages. Versions are pinned in the [benchmarks/go.mod](https://github.com/uber-go/zap/raw/master/benchmarks/go.mod) file. [↩](https://pkg.go.dev/go.uber.org/zap#readme-anchor-versions)

## ![](https://pkg.go.dev/static/shared/icon/code_gm_grey_24dp.svg) Documentation

[Rendered for](https://go.dev/about#build-context)

### Overview

Package zap provides fast, structured, leveled logging.

For applications that log in the hot path, reflection-based serialization and string formatting are prohibitively expensive - they're CPU-intensive and make many small allocations. Put differently, using json.Marshal and fmt.Fprintf to log tons of interface{} makes your application slow.

Zap takes a different approach. It includes a reflection-free, zero-allocation JSON encoder, and the base Logger strives to avoid serialization overhead and allocations wherever possible. By building the high-level SugaredLogger on that foundation, zap lets users choose when they need to count every allocation and when they'd prefer a more familiar, loosely typed API.

#### Choosing a Logger

In contexts where performance is nice, but not critical, use the SugaredLogger. It's 4-10x faster than other structured logging packages and supports both structured and printf-style logging. Like log15 and go-kit, the SugaredLogger's structured logging APIs are loosely typed and accept a variadic number of key-value pairs. (For more advanced use cases, they also accept strongly typed fields - see the SugaredLogger.With documentation for details.)

```
sugar := zap.NewExample().Sugar()
defer sugar.Sync()
sugar.Infow("failed to fetch URL",
  "url", "http://example.com",
  "attempt", 3,
  "backoff", time.Second,
)
sugar.Infof("failed to fetch URL: %s", "http://example.com")
```
By default, loggers are unbuffered. However, since zap's low-level APIs allow buffering, calling Sync before letting your process exit is a good habit.

In the rare contexts where every microsecond and every allocation matter, use the Logger. It's even faster than the SugaredLogger and allocates far less, but it only supports strongly-typed, structured logging.

```
logger := zap.NewExample()
defer logger.Sync()
logger.Info("failed to fetch URL",
  zap.String("url", "http://example.com"),
  zap.Int("attempt", 3),
  zap.Duration("backoff", time.Second),
)
```
Choosing between the Logger and SugaredLogger doesn't need to be an application-wide decision: converting between the two is simple and inexpensive.

```
logger := zap.NewExample()
defer logger.Sync()
sugar := logger.Sugar()
plain := sugar.Desugar()
```
#### Configuring Zap

The simplest way to build a Logger is to use zap's opinionated presets: NewExample, NewProduction, and NewDevelopment. These presets build a logger with a single function call:

```
logger, err := zap.NewProduction()
if err != nil {
  log.Fatalf("can't initialize zap logger: %v", err)
}
defer logger.Sync()
```
Presets are fine for small projects, but larger projects and organizations naturally require a bit more customization. For most users, zap's Config struct strikes the right balance between flexibility and convenience. See the package-level BasicConfiguration example for sample code.

More unusual configurations (splitting output between files, sending logs to a message queue, etc.) are possible, but require direct use of go.uber.org/zap/zapcore. See the package-level AdvancedConfiguration example for sample code.

#### Extending Zap

The zap package itself is a relatively thin wrapper around the interfaces in go.uber.org/zap/zapcore. Extending zap to support a new encoding (e.g., BSON), a new log sink (e.g., Kafka), or something more exotic (perhaps an exception aggregation service, like Sentry or Rollbar) typically requires implementing the zapcore.Encoder, zapcore.WriteSyncer, or zapcore.Core interfaces. See the zapcore documentation for details.

Similarly, package authors can use the high-performance Encoder and Core implementations in the zapcore package to build their own loggers.

#### Frequently Asked Questions

An FAQ covering everything from installation errors to design decisions is available at [https://github.com/uber-go/zap/blob/master/FAQ.md](https://github.com/uber-go/zap/blob/master/FAQ.md).

Example (AdvancedConfiguration) [¶](https://pkg.go.dev/go.uber.org/zap#example-package-AdvancedConfiguration "Go to Example (AdvancedConfiguration)")

```
package main

import (
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// The bundled Config struct only supports the most common configuration
	// options. More complex needs, like splitting logs between multiple files
	// or writing to non-file outputs, require use of the zapcore package.
	//
	// In this example, imagine we're both sending our logs to Kafka and writing
	// them to the console. We'd like to encode the console output and the Kafka
	// topics differently, and we'd also like special treatment for
	// high-priority logs.

	// First, define our level-handling logic.
	highPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zapcore.ErrorLevel
	})
	lowPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl < zapcore.ErrorLevel
	})

	// Assume that we have clients for two Kafka topics. The clients implement
	// zapcore.WriteSyncer and are safe for concurrent use. (If they only
	// implement io.Writer, we can use zapcore.AddSync to add a no-op Sync
	// method. If they're not safe for concurrent use, we can add a protecting
	// mutex with zapcore.Lock.)
	topicDebugging := zapcore.AddSync(io.Discard)
	topicErrors := zapcore.AddSync(io.Discard)

	// High-priority output should also go to standard error, and low-priority
	// output should also go to standard out.
	consoleDebugging := zapcore.Lock(os.Stdout)
	consoleErrors := zapcore.Lock(os.Stderr)

	// Optimize the Kafka output for machine consumption and the console output
	// for human operators.
	kafkaEncoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	consoleEncoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())

	// Join the outputs, encoders, and level-handling functions into
	// zapcore.Cores, then tee the four cores together.
	core := zapcore.NewTee(
		zapcore.NewCore(kafkaEncoder, topicErrors, highPriority),
		zapcore.NewCore(consoleEncoder, consoleErrors, highPriority),
		zapcore.NewCore(kafkaEncoder, topicDebugging, lowPriority),
		zapcore.NewCore(consoleEncoder, consoleDebugging, lowPriority),
	)

	// From a zapcore.Core, it's easy to construct a Logger.
	logger := zap.New(core)
	defer logger.Sync()
	logger.Info("constructed a logger")
}
```
```

```
Example (BasicConfiguration) [¶](https://pkg.go.dev/go.uber.org/zap#example-package-BasicConfiguration "Go to Example (BasicConfiguration)")

```
package main

import (
	"encoding/json"

	"go.uber.org/zap"
)

func main() {
	// For some users, the presets offered by the NewProduction, NewDevelopment,
	// and NewExample constructors won't be appropriate. For most of those
	// users, the bundled Config struct offers the right balance of flexibility
	// and convenience. (For more complex needs, see the AdvancedConfiguration
	// example.)
	//
	// See the documentation for Config and zapcore.EncoderConfig for all the
	// available options.
	rawJSON := []byte(`{
	  "level": "debug",
	  "encoding": "json",
	  "outputPaths": ["stdout", "/tmp/logs"],
	  "errorOutputPaths": ["stderr"],
	  "initialFields": {"foo": "bar"},
	  "encoderConfig": {
	    "messageKey": "message",
	    "levelKey": "level",
	    "levelEncoder": "lowercase"
	  }
	}`)

	var cfg zap.Config
	if err := json.Unmarshal(rawJSON, &cfg); err != nil {
		panic(err)
	}
	logger := zap.Must(cfg.Build())
	defer logger.Sync()

	logger.Info("logger construction succeeded")
}
```
```
Output:
{"level":"info","message":"logger construction succeeded","foo":"bar"}
```
Example (Presets) [¶](https://pkg.go.dev/go.uber.org/zap#example-package-Presets "Go to Example (Presets)")

```
package main

import (
	"time"

	"go.uber.org/zap"
)

func main() {
	// Using zap's preset constructors is the simplest way to get a feel for the
	// package, but they don't allow much customization.
	logger := zap.NewExample() // or NewProduction, or NewDevelopment
	defer logger.Sync()

	const url = "http://example.com"

	// In most circumstances, use the SugaredLogger. It's 4-10x faster than most
	// other structured logging packages and has a familiar, loosely-typed API.
	sugar := logger.Sugar()
	sugar.Infow("Failed to fetch URL.",
		// Structured context as loosely typed key-value pairs.
		"url", url,
		"attempt", 3,
		"backoff", time.Second,
	)
	sugar.Infof("Failed to fetch URL: %s", url)

	// In the unusual situations where every microsecond matters, use the
	// Logger. It's even faster than the SugaredLogger, but only supports
	// structured logging.
	logger.Info("Failed to fetch URL.",
		// Structured context as strongly typed fields.
		zap.String("url", url),
		zap.Int("attempt", 3),
		zap.Duration("backoff", time.Second),
	)
}
```
```
Output:
{"level":"info","msg":"Failed to fetch URL.","url":"http://example.com","attempt":3,"backoff":"1s"}
{"level":"info","msg":"Failed to fetch URL: http://example.com"}
{"level":"info","msg":"Failed to fetch URL.","url":"http://example.com","attempt":3,"backoff":"1s"}
```
### Examples

- [Package (AdvancedConfiguration)](https://pkg.go.dev/go.uber.org/zap#example-package-AdvancedConfiguration)
- [Package (BasicConfiguration)](https://pkg.go.dev/go.uber.org/zap#example-package-BasicConfiguration)
- [Package (Presets)](https://pkg.go.dev/go.uber.org/zap#example-package-Presets)
- [AtomicLevel](https://pkg.go.dev/go.uber.org/zap#example-AtomicLevel)
- [AtomicLevel (Config)](https://pkg.go.dev/go.uber.org/zap#example-AtomicLevel-Config)
- [Dict](https://pkg.go.dev/go.uber.org/zap#example-Dict)
- [DictObject](https://pkg.go.dev/go.uber.org/zap#example-DictObject)
- [Logger.Check](https://pkg.go.dev/go.uber.org/zap#example-Logger.Check)
- [Logger.Named](https://pkg.go.dev/go.uber.org/zap#example-Logger.Named)
- [Namespace](https://pkg.go.dev/go.uber.org/zap#example-Namespace)
- [NewStdLog](https://pkg.go.dev/go.uber.org/zap#example-NewStdLog)
- [Object](https://pkg.go.dev/go.uber.org/zap#example-Object)
- [ObjectValues](https://pkg.go.dev/go.uber.org/zap#example-ObjectValues)
- [Objects](https://pkg.go.dev/go.uber.org/zap#example-Objects)
- [RedirectStdLog](https://pkg.go.dev/go.uber.org/zap#example-RedirectStdLog)
- [ReplaceGlobals](https://pkg.go.dev/go.uber.org/zap#example-ReplaceGlobals)
- [WrapCore (Replace)](https://pkg.go.dev/go.uber.org/zap#example-WrapCore-Replace)
- [WrapCore (Wrap)](https://pkg.go.dev/go.uber.org/zap#example-WrapCore-Wrap)

### Constants

[View Source](https://github.com/uber-go/zap/blob/v1.28.0/level.go#L30)

```
const (
	// DebugLevel logs are typically voluminous, and are usually disabled in
	// production.
	DebugLevel = zapcore.DebugLevel
	// InfoLevel is the default logging priority.
	InfoLevel = zapcore.InfoLevel
	// WarnLevel logs are more important than Info, but don't need individual
	// human review.
	WarnLevel = zapcore.WarnLevel
	// ErrorLevel logs are high-priority. If an application is running smoothly,
	// it shouldn't generate any error-level logs.
	ErrorLevel = zapcore.ErrorLevel
	// DPanicLevel logs are particularly important errors. In development the
	// logger panics after writing the message.
	DPanicLevel = zapcore.DPanicLevel
	// PanicLevel logs a message, then panics.
	PanicLevel = zapcore.PanicLevel
	// FatalLevel logs a message, then calls os.Exit(1).
	FatalLevel = zapcore.FatalLevel
)
```
### Variables

This section is empty.

### Functions

#### func [CombineWriteSyncers](https://github.com/uber-go/zap/blob/v1.28.0/writer.go#L93)

```
func CombineWriteSyncers(writers ...zapcore.WriteSyncer) zapcore.WriteSyncer
```
CombineWriteSyncers is a utility that combines multiple WriteSyncers into a single, locked WriteSyncer. If no inputs are supplied, it returns a no-op WriteSyncer.

It's provided purely as a convenience; the result is no different from using zapcore.NewMultiWriteSyncer and zapcore.Lock individually.

#### func [DictObject](https://github.com/uber-go/zap/blob/v1.28.0/field.go#L440) added in v1.27.1

```
func DictObject(val ...Field) zapcore.ObjectMarshaler
```
DictObject constructs a [zapcore.ObjectMarshaler](https://pkg.go.dev/go.uber.org/zap@v1.28.0/zapcore#ObjectMarshaler) with the given list of fields. The resulting object marshaler can be used as input to [Object](https://pkg.go.dev/go.uber.org/zap#Object), [Objects](https://pkg.go.dev/go.uber.org/zap#Objects), or any other functions that expect an object marshaler.

Example [¶](https://pkg.go.dev/go.uber.org/zap#example-DictObject "Go to Example")

```
package main

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	logger := zap.NewExample()
	defer logger.Sync()

	// Use DictObject to create zapcore.ObjectMarshaler implementations from Field arrays,
	// then use the Object and Objects field constructors to turn them back into a Field.

	logger.Debug("worker received job",
		zap.Object("w1",
			zap.DictObject(
				zap.Int("id", 402000),
				zap.String("description", "compress image data"),
				zap.Int("priority", 3),
			),
		))

	d1 := 68 * time.Millisecond
	d2 := 79 * time.Millisecond
	d3 := 57 * time.Millisecond

	logger.Info("worker status checks",
		zap.Objects("job batch enqueued",
			[]zapcore.ObjectMarshaler{
				zap.DictObject(
					zap.String("worker", "w1"),
					zap.Int("load", 419),
					zap.Duration("latency", d1),
				),
				zap.DictObject(
					zap.String("worker", "w2"),
					zap.Int("load", 520),
					zap.Duration("latency", d2),
				),
				zap.DictObject(
					zap.String("worker", "w3"),
					zap.Int("load", 310),
					zap.Duration("latency", d3),
				),
			},
		))
}
```
```
Output:
{"level":"debug","msg":"worker received job","w1":{"id":402000,"description":"compress image data","priority":3}}
{"level":"info","msg":"worker status checks","job batch enqueued":[{"worker":"w1","load":419,"latency":"68ms"},{"worker":"w2","load":520,"latency":"79ms"},{"worker":"w3","load":310,"latency":"57ms"}]}
```
#### func [LevelFlag](https://github.com/uber-go/zap/blob/v1.28.0/flag.go#L35)

```
func LevelFlag(name string, defaultLevel zapcore.Level, usage string) *zapcore.Level
```
LevelFlag uses the standard library's flag.Var to declare a global flag with the specified name, default, and usage guidance. The returned value is a pointer to the value of the flag.

If you don't want to use the flag package's global state, you can use any non-nil \*Level as a flag.Value with your own \*flag.FlagSet.

#### func [NewDevelopmentEncoderConfig](https://github.com/uber-go/zap/blob/v1.28.0/config.go#L200)

```
func NewDevelopmentEncoderConfig() zapcore.EncoderConfig
```
NewDevelopmentEncoderConfig returns an opinionated EncoderConfig for development environments.

Messages encoded with this configuration will use Zap's console encoder intended to print human-readable output. It will print log messages with the following information:

- The log level (e.g. "INFO", "ERROR").
- The time in ISO8601 format (e.g. "2017-01-01T12:00:00Z").
- The message passed to the log statement.
- If available, a short path to the file and line number where the log statement was issued. The logger configuration determines whether this field is captured.
- If available, a stacktrace from the line where the log statement was issued. The logger configuration determines whether this field is captured.

By default, the following formats are used for different types:

- Time is formatted in ISO8601 format (e.g. "2017-01-01T12:00:00Z").
- Duration is formatted as a string (e.g. "1.234s").

You may change these by setting the appropriate fields in the returned object. For example, use the following to change the time encoding format:

```
cfg := zap.NewDevelopmentEncoderConfig()
cfg.EncodeTime = zapcore.ISO8601TimeEncoder
```
#### func [NewProductionEncoderConfig](https://github.com/uber-go/zap/blob/v1.28.0/config.go#L124)

```
func NewProductionEncoderConfig() zapcore.EncoderConfig
```
NewProductionEncoderConfig returns an opinionated EncoderConfig for production environments.

Messages encoded with this configuration will be JSON-formatted and will have the following keys by default:

- "level": The logging level (e.g. "info", "error").
- "ts": The current time in number of seconds since the Unix epoch.
- "msg": The message passed to the log statement.
- "caller": If available, a short path to the file and line number where the log statement was issued. The logger configuration determines whether this field is captured.
- "stacktrace": If available, a stack trace from the line where the log statement was issued. The logger configuration determines whether this field is captured.

By default, the following formats are used for different types:

- Time is formatted as floating-point number of seconds since the Unix epoch.
- Duration is formatted as floating-point number of seconds.

You may change these by setting the appropriate fields in the returned object. For example, use the following to change the time encoding format:

```
cfg := zap.NewProductionEncoderConfig()
cfg.EncodeTime = zapcore.ISO8601TimeEncoder
```
#### func [NewStdLog](https://github.com/uber-go/zap/blob/v1.28.0/global.go#L78)

```
func NewStdLog(l *Logger) *log.Logger
```
NewStdLog returns a \*log.Logger which writes to the supplied zap Logger at InfoLevel. To redirect the standard library's package-global logging functions, use RedirectStdLog instead.

Example [¶](https://pkg.go.dev/go.uber.org/zap#example-NewStdLog "Go to Example")

```
package main

import (
	"go.uber.org/zap"
)

func main() {
	logger := zap.NewExample()
	defer logger.Sync()

	std := zap.NewStdLog(logger)
	std.Print("standard logger wrapper")
}
```
```
Output:
{"level":"info","msg":"standard logger wrapper"}
```
#### func [NewStdLogAt](https://github.com/uber-go/zap/blob/v1.28.0/global.go#L86) added in v1.7.0

```
func NewStdLogAt(l *Logger, level zapcore.Level) (*log.Logger, error)
```
NewStdLogAt returns \*log.Logger which writes to supplied zap logger at required level.

### Types

#### type [AtomicLevel](https://github.com/uber-go/zap/blob/v1.28.0/level.go#L71)

```
type AtomicLevel struct {
	// contains filtered or unexported fields
}
```
An AtomicLevel is an atomically changeable, dynamic logging level. It lets you safely change the log level of a tree of loggers (the root logger and any children created by adding context) at runtime.

The AtomicLevel itself is an http.Handler that serves a JSON endpoint to alter its level.

AtomicLevels must be created with the NewAtomicLevel constructor to allocate their internal atomic pointer.

Example [¶](https://pkg.go.dev/go.uber.org/zap#example-AtomicLevel "Go to Example")

```
package main

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	atom := zap.NewAtomicLevel()

	// To keep the example deterministic, disable timestamps in the output.
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = ""

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))
	defer logger.Sync()

	logger.Info("info logging enabled")

	atom.SetLevel(zap.ErrorLevel)
	logger.Info("info logging disabled")
}
```
```
Output:
{"level":"info","msg":"info logging enabled"}
```
Example (Config) [¶](https://pkg.go.dev/go.uber.org/zap#example-AtomicLevel-Config "Go to Example (Config)")

```
package main

import (
	"encoding/json"

	"go.uber.org/zap"
)

func main() {
	// The zap.Config struct includes an AtomicLevel. To use it, keep a
	// reference to the Config.
	rawJSON := []byte(`{
		"level": "info",
		"outputPaths": ["stdout"],
		"errorOutputPaths": ["stderr"],
		"encoding": "json",
		"encoderConfig": {
			"messageKey": "message",
			"levelKey": "level",
			"levelEncoder": "lowercase"
		}
	}`)
	var cfg zap.Config
	if err := json.Unmarshal(rawJSON, &cfg); err != nil {
		panic(err)
	}
	logger := zap.Must(cfg.Build())
	defer logger.Sync()

	logger.Info("info logging enabled")

	cfg.Level.SetLevel(zap.ErrorLevel)
	logger.Info("info logging disabled")
}
```
```
Output:
{"level":"info","message":"info logging enabled"}
```
#### type [Logger](https://github.com/uber-go/zap/blob/v1.28.0/logger.go#L41)

```
type Logger struct {
	// contains filtered or unexported fields
}
```
A Logger provides fast, leveled, structured logging. All methods are safe for concurrent use.

The Logger is designed for contexts in which every microsecond and every allocation matters, so its API intentionally favors performance and type safety over brevity. For most applications, the SugaredLogger strikes a better balance between performance and ergonomics.

#### func (\*Logger) [Sugar](https://github.com/uber-go/zap/blob/v1.28.0/logger.go#L146)

```
func (log *Logger) Sugar() *SugaredLogger
```
Sugar wraps the Logger to provide a more ergonomic, but slightly slower, API. Sugaring a Logger is quite inexpensive, so it's reasonable for a single application to use both Loggers and SugaredLoggers, converting between them on the boundaries of performance-sensitive code.

#### type [SugaredLogger](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L55)

```
type SugaredLogger struct {
	// contains filtered or unexported fields
}
```
A SugaredLogger wraps the base Logger functionality in a slower, but less verbose, API. Any Logger can be converted to a SugaredLogger with its Sugar method.

Unlike the Logger, the SugaredLogger doesn't insist on structured logging. For each log level, it exposes four methods:

- methods named after the log level for log.Print-style logging
- methods ending in "w" for loosely-typed structured logging
- methods ending in "f" for log.Printf-style logging
- methods ending in "ln" for log.Println-style logging

For example, the methods for InfoLevel are:

```
Info(...any)           Print-style logging
Infow(...any)          Structured logging (read as "info with")
Infof(string, ...any)  Printf-style logging
Infoln(...any)         Println-style logging
```
#### func [S](https://github.com/uber-go/zap/blob/v1.28.0/global.go#L57)

```
func S() *SugaredLogger
```
S returns the global SugaredLogger, which can be reconfigured with ReplaceGlobals. It's safe for concurrent use.

#### func (\*SugaredLogger) [DPanic](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L173)

```
func (s *SugaredLogger) DPanic(args ...interface{})
```
DPanic logs the provided arguments at [DPanicLevel](https://pkg.go.dev/go.uber.org/zap#DPanicLevel). In development, the logger then panics. (See [DPanicLevel](https://pkg.go.dev/go.uber.org/zap#DPanicLevel) for details.) Spaces are added between arguments when neither is a string.

#### func (\*SugaredLogger) [DPanicf](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L222)

```
func (s *SugaredLogger) DPanicf(template string, args ...interface{})
```
DPanicf formats the message according to the format specifier and logs it at [DPanicLevel](https://pkg.go.dev/go.uber.org/zap#DPanicLevel). In development, the logger then panics. (See [DPanicLevel](https://pkg.go.dev/go.uber.org/zap#DPanicLevel) for details.)

#### func (\*SugaredLogger) [DPanicln](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L324) added in v1.22.0

```
func (s *SugaredLogger) DPanicln(args ...interface{})
```
DPanicln logs a message at [DPanicLevel](https://pkg.go.dev/go.uber.org/zap#DPanicLevel). In development, the logger then panics. (See [DPanicLevel](https://pkg.go.dev/go.uber.org/zap#DPanicLevel) for details.) Spaces are always added between arguments.

#### func (\*SugaredLogger) [DPanicw](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L275)

```
func (s *SugaredLogger) DPanicw(msg string, keysAndValues ...interface{})
```
DPanicw logs a message with some additional context. In development, the logger then panics. (See DPanicLevel for details.) The variadic key-value pairs are treated as they are in With.

#### func (\*SugaredLogger) [Debug](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L148)

```
func (s *SugaredLogger) Debug(args ...interface{})
```
Debug logs the provided arguments at [DebugLevel](https://pkg.go.dev/go.uber.org/zap#DebugLevel). Spaces are added between arguments when neither is a string.

#### func (\*SugaredLogger) [Debugf](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L197)

```
func (s *SugaredLogger) Debugf(template string, args ...interface{})
```
Debugf formats the message according to the format specifier and logs it at [DebugLevel](https://pkg.go.dev/go.uber.org/zap#DebugLevel).

#### func (\*SugaredLogger) [Debugln](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L299) added in v1.22.0

```
func (s *SugaredLogger) Debugln(args ...interface{})
```
Debugln logs a message at [DebugLevel](https://pkg.go.dev/go.uber.org/zap#DebugLevel). Spaces are always added between arguments.

#### func (\*SugaredLogger) [Debugw](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L250)

```
func (s *SugaredLogger) Debugw(msg string, keysAndValues ...interface{})
```
Debugw logs a message with some additional context. The variadic key-value pairs are treated as they are in With.

When debug-level logging is disabled, this is much faster than

```
s.With(keysAndValues).Debug(msg)
```
#### func (\*SugaredLogger) [Desugar](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L63)

```
func (s *SugaredLogger) Desugar() *Logger
```
Desugar unwraps a SugaredLogger, exposing the original Logger. Desugaring is quite inexpensive, so it's reasonable for a single application to use both Loggers and SugaredLoggers, converting between them on the boundaries of performance-sensitive code.

#### func (\*SugaredLogger) [Error](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L166)

```
func (s *SugaredLogger) Error(args ...interface{})
```
Error logs the provided arguments at [ErrorLevel](https://pkg.go.dev/go.uber.org/zap#ErrorLevel). Spaces are added between arguments when neither is a string.

#### func (\*SugaredLogger) [Errorf](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L215)

```
func (s *SugaredLogger) Errorf(template string, args ...interface{})
```
Errorf formats the message according to the format specifier and logs it at [ErrorLevel](https://pkg.go.dev/go.uber.org/zap#ErrorLevel).

#### func (\*SugaredLogger) [Errorln](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L317) added in v1.22.0

```
func (s *SugaredLogger) Errorln(args ...interface{})
```
Errorln logs a message at [ErrorLevel](https://pkg.go.dev/go.uber.org/zap#ErrorLevel). Spaces are always added between arguments.

#### func (\*SugaredLogger) [Errorw](https://github.com/uber-go/zap/blob/v1.28.0/sugar.go#L268)

```
func (s *SugaredLogger) Errorw(msg string, keysAndValues ...interface{})
```
Errorw logs a message with some additional context. The variadic key-value pairs are treated as they are in With.

## ![](https://pkg.go.dev/static/shared/icon/insert_drive_file_gm_grey_24dp.svg) Source Files

[View all Source files](https://github.com/uber-go/zap/tree/v1.28.0)

## ![](https://pkg.go.dev/static/shared/icon/folder_gm_grey_24dp.svg) Directories

|Path|Synopsis|
|---|---|
|[benchmarks](https://pkg.go.dev/go.uber.org/zap/benchmarks) module||
|[buffer](https://pkg.go.dev/go.uber.org/zap@v1.28.0/buffer)<br /><br /><br /><br />Package buffer provides a thin wrapper around a byte slice.|Package buffer provides a thin wrapper around a byte slice.|
|[exp](https://pkg.go.dev/go.uber.org/zap/exp) module||
|[internal](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal)<br /><br /><br /><br />Package internal and its subpackages hold types and functionality that are not part of Zap's public API.|Package internal and its subpackages hold types and functionality that are not part of Zap's public API.|
|[bufferpool](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal/bufferpool)<br /><br />Package bufferpool houses zap's shared internal buffer pool.|Package bufferpool houses zap's shared internal buffer pool.|
|[color](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal/color)<br /><br />Package color adds coloring functionality for TTY output.|Package color adds coloring functionality for TTY output.|
|[exit](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal/exit)<br /><br />Package exit provides stubs so that unit tests can exercise code that calls os.Exit(1).|Package exit provides stubs so that unit tests can exercise code that calls os.Exit(1).|
|[pool](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal/pool)<br /><br />Package pool provides internal pool utilities.|Package pool provides internal pool utilities.|
|[readme](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal/readme) command<br /><br />readme generates Zap's README from a template.|readme generates Zap's README from a template.|
|[stacktrace](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal/stacktrace)<br /><br />Package stacktrace provides support for gathering stack traces efficiently.|Package stacktrace provides support for gathering stack traces efficiently.|
|[ztest](https://pkg.go.dev/go.uber.org/zap@v1.28.0/internal/ztest)<br /><br />Package ztest provides low-level helpers for testing log output.|Package ztest provides low-level helpers for testing log output.|
|[zapcore](https://pkg.go.dev/go.uber.org/zap@v1.28.0/zapcore)<br /><br /><br /><br />Package zapcore defines and implements the low-level interfaces upon which zap is built.|Package zapcore defines and implements the low-level interfaces upon which zap is built.|
|[zapgrpc](https://pkg.go.dev/go.uber.org/zap@v1.28.0/zapgrpc)<br /><br /><br /><br />Package zapgrpc provides a logger that is compatible with grpclog.|Package zapgrpc provides a logger that is compatible with grpclog.|
|[internal/test](https://pkg.go.dev/go.uber.org/zap/zapgrpc/internal/test) module||
|[zapio](https://pkg.go.dev/go.uber.org/zap@v1.28.0/zapio)<br /><br /><br /><br />Package zapio provides tools for interacting with IO streams through Zap.|Package zapio provides tools for interacting with IO streams through Zap.|
|[zaptest](https://pkg.go.dev/go.uber.org/zap@v1.28.0/zaptest)<br /><br /><br /><br />Package zaptest provides a variety of helpers for testing log output.|Package zaptest provides a variety of helpers for testing log output.|
|[observer](https://pkg.go.dev/go.uber.org/zap@v1.28.0/zaptest/observer)<br /><br />Package observer provides a zapcore.Core that keeps an in-memory, encoding-agnostic representation of log entries.|Package observer provides a zapcore.Core that keeps an in-memory, encoding-agnostic representation of log entries.|

