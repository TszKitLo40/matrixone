// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logutil

import (
	"context"
	"io"
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

var gLogConfigs atomic.Value

func EnableStoreDB() bool {
	return !gLogConfigs.Load().(*LogConfig).DisableStore
}

func setGlobalLogConfig(cfg *LogConfig) {
	gLogConfigs.Store(cfg)
}

func getGlobalLogConfig() *LogConfig {
	return gLogConfigs.Load().(*LogConfig)
}

type ZapSink struct {
	enc zapcore.Encoder
	out zapcore.WriteSyncer
}

// logReporter should be trace.ReportLog
var logReporter atomic.Value

// zapReporter should be trace.ReportZap
var zapReporter atomic.Value

// levelChangeFunc should be trace.SetLogLevel function
var levelChangeFunc atomic.Value

// contextField should be trace.ContextField function
var contextField atomic.Value

type TraceReporter struct {
	ReportLog    reportLogFunc
	ReportZap    reportZapFunc
	LevelSignal  levelChangeSignal
	ContextField contextFieldFunc
}

type reportLogFunc func(context.Context, zapcore.Level, int, string, ...any)
type reportZapFunc func(zapcore.Encoder, zapcore.Entry, []zapcore.Field) (*buffer.Buffer, error)
type levelChangeSignal func(zapcore.LevelEnabler)
type contextFieldFunc func(context.Context) zap.Field

func noopReportLog(context.Context, zapcore.Level, int, string, ...any) {}
func noopReportZap(zapcore.Encoder, zapcore.Entry, []zapcore.Field) (*buffer.Buffer, error) {
	return buffer.NewPool().Get(), nil
}
func noopLevelSignal(zapcore.LevelEnabler)       {}
func noopContextField(context.Context) zap.Field { return zap.String("span", "{}") }

func SetLogReporter(r *TraceReporter) {
	if r.ReportLog != nil {
		logReporter.Store(r.ReportLog)
	}
	if r.ReportZap != nil {
		zapReporter.Store(r.ReportZap)
	}
	if r.LevelSignal != nil {
		levelChangeFunc.Store(r.LevelSignal)
	}
	if r.ContextField != nil {
		contextField.Store(r.ContextField)
	}
}

func GetReportLogFunc() reportLogFunc {
	return logReporter.Load().(reportLogFunc)
}

func GetReportZapFunc() reportZapFunc {
	return zapReporter.Load().(reportZapFunc)
}

func GetLevelChangeFunc() levelChangeSignal {
	return levelChangeFunc.Load().(levelChangeSignal)
}

func GetContextFieldFunc() contextFieldFunc {
	return contextField.Load().(contextFieldFunc)
}

var _ zapcore.Encoder = (*TraceLogEncoder)(nil)

type TraceLogEncoder struct {
	zapcore.Encoder
	spanContextField zap.Field
}

func (e *TraceLogEncoder) Clone() zapcore.Encoder {
	return &TraceLogEncoder{
		Encoder: e.Encoder.Clone(),
	}
}

var SpanFieldKey atomic.Value

func (e *TraceLogEncoder) AddObject(key string, val zapcore.ObjectMarshaler) error {
	if key == SpanFieldKey.Load().(string) {
		//e.sp = obj.(*trace.SpanContext)
		e.spanContextField = zap.Object(key, val)
		return nil
	}
	return e.Encoder.AddObject(key, val)
}

func (e *TraceLogEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	if e.spanContextField.Key == SpanFieldKey.Load().(string) {
		fields = append(fields, e.spanContextField)
	}
	for _, v := range fields {
		if v.Type == zapcore.BoolType && v.Key == MOInternalFiledKeyNoopReport {
			return e.Encoder.EncodeEntry(entry, fields[:0])
		}
	}
	return GetReportZapFunc()(e.Encoder, entry, fields)
}

const MOInternalFiledKeyNoopReport = "MOInternalFiledKeyNoopReport"

func NoReportFiled() zap.Field {
	return zap.Bool(MOInternalFiledKeyNoopReport, true)
}

func ErrorField(err error) zap.Field {
	return zap.Error(err)
}

func newTraceLogEncoder() *TraceLogEncoder {
	// default like zap.NewProductionEncoderConfig(), but clean core-elems ENCODE
	e := &TraceLogEncoder{
		Encoder: zapcore.NewJSONEncoder(
			zapcore.EncoderConfig{
				StacktraceKey:  "stacktrace",
				LineEnding:     zapcore.DefaultLineEnding,
				EncodeLevel:    zapcore.LowercaseLevelEncoder,
				EncodeTime:     zapcore.EpochTimeEncoder,
				EncodeDuration: zapcore.SecondsDurationEncoder,
				EncodeCaller:   zapcore.ShortCallerEncoder,
			}),
	}
	return e
}

func getTraceLogSinks() (zapcore.Encoder, zapcore.WriteSyncer) {
	return newTraceLogEncoder(), zapcore.AddSync(io.Discard)
}

func init() {
	SpanFieldKey.Store("")
}
