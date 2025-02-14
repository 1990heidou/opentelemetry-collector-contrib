// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package kafkaexporter

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/jaeger"
	"testing"

	"github.com/IBM/sarama"
	"github.com/IBM/sarama/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/testdata"
)

func TestNewExporter_err_version(t *testing.T) {
	c := Config{ProtocolVersion: "0.0.0", Encoding: defaultEncoding}
	texp, err := newTracesExporter(c, exportertest.NewNopCreateSettings(), tracesMarshalers())
	assert.Error(t, err)
	assert.Nil(t, texp)
}

func TestNewExporter_err_encoding(t *testing.T) {
	c := Config{Encoding: "foo"}
	texp, err := newTracesExporter(c, exportertest.NewNopCreateSettings(), tracesMarshalers())
	assert.EqualError(t, err, errUnrecognizedEncoding.Error())
	assert.Nil(t, texp)
}

func TestNewMetricsExporter_err_version(t *testing.T) {
	c := Config{ProtocolVersion: "0.0.0", Encoding: defaultEncoding}
	mexp, err := newMetricsExporter(c, exportertest.NewNopCreateSettings(), metricsMarshalers())
	assert.Error(t, err)
	assert.Nil(t, mexp)
}

func TestNewMetricsExporter_err_encoding(t *testing.T) {
	c := Config{Encoding: "bar"}
	mexp, err := newMetricsExporter(c, exportertest.NewNopCreateSettings(), metricsMarshalers())
	assert.EqualError(t, err, errUnrecognizedEncoding.Error())
	assert.Nil(t, mexp)
}

func TestNewMetricsExporter_err_traces_encoding(t *testing.T) {
	c := Config{Encoding: "jaeger_proto"}
	mexp, err := newMetricsExporter(c, exportertest.NewNopCreateSettings(), metricsMarshalers())
	assert.EqualError(t, err, errUnrecognizedEncoding.Error())
	assert.Nil(t, mexp)
}

func TestNewLogsExporter_err_version(t *testing.T) {
	c := Config{ProtocolVersion: "0.0.0", Encoding: defaultEncoding}
	mexp, err := newLogsExporter(c, exportertest.NewNopCreateSettings(), logsMarshalers())
	assert.Error(t, err)
	assert.Nil(t, mexp)
}

func TestNewLogsExporter_err_encoding(t *testing.T) {
	c := Config{Encoding: "bar"}
	mexp, err := newLogsExporter(c, exportertest.NewNopCreateSettings(), logsMarshalers())
	assert.EqualError(t, err, errUnrecognizedEncoding.Error())
	assert.Nil(t, mexp)
}

func TestNewLogsExporter_err_traces_encoding(t *testing.T) {
	c := Config{Encoding: "jaeger_proto"}
	mexp, err := newLogsExporter(c, exportertest.NewNopCreateSettings(), logsMarshalers())
	assert.EqualError(t, err, errUnrecognizedEncoding.Error())
	assert.Nil(t, mexp)
}

func TestNewExporter_err_auth_type(t *testing.T) {
	c := Config{
		ProtocolVersion: "2.0.0",
		Authentication: Authentication{
			TLS: &configtls.TLSClientSetting{
				TLSSetting: configtls.TLSSetting{
					CAFile: "/doesnotexist",
				},
			},
		},
		Encoding: defaultEncoding,
		Metadata: Metadata{
			Full: false,
		},
		Producer: Producer{
			Compression: "none",
		},
	}
	texp, err := newTracesExporter(c, exportertest.NewNopCreateSettings(), tracesMarshalers())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load TLS config")
	assert.Nil(t, texp)
	mexp, err := newMetricsExporter(c, exportertest.NewNopCreateSettings(), metricsMarshalers())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load TLS config")
	assert.Nil(t, mexp)
	lexp, err := newLogsExporter(c, exportertest.NewNopCreateSettings(), logsMarshalers())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load TLS config")
	assert.Nil(t, lexp)

}

func TestNewExporter_err_compression(t *testing.T) {
	c := Config{
		Encoding: defaultEncoding,
		Producer: Producer{
			Compression: "idk",
		},
	}
	texp, err := newTracesExporter(c, exportertest.NewNopCreateSettings(), tracesMarshalers())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "producer.compression should be one of 'none', 'gzip', 'snappy', 'lz4', or 'zstd'. configured value idk")
	assert.Nil(t, texp)
}

func TestTracesPusher(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)
	producer.ExpectSendMessageAndSucceed()

	p := kafkaTracesProducer{
		producer:  producer,
		marshaler: newPdataTracesMarshaler(&ptrace.ProtoMarshaler{}, defaultEncoding),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	err := p.tracesPusher(context.Background(), testdata.GenerateTracesTwoSpansSameResource())
	require.NoError(t, err)
}

func TestTracesPusher_err(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)
	expErr := fmt.Errorf("failed to send")
	producer.ExpectSendMessageAndFail(expErr)

	p := kafkaTracesProducer{
		producer:  producer,
		marshaler: newPdataTracesMarshaler(&ptrace.ProtoMarshaler{}, defaultEncoding),
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	td := testdata.GenerateTracesTwoSpansSameResource()
	err := p.tracesPusher(context.Background(), td)
	assert.EqualError(t, err, expErr.Error())
}

func TestTracesPusher_marshal_error(t *testing.T) {
	expErr := fmt.Errorf("failed to marshal")
	p := kafkaTracesProducer{
		marshaler: &tracesErrorMarshaler{err: expErr},
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	td := testdata.GenerateTracesTwoSpansSameResource()
	err := p.tracesPusher(context.Background(), td)
	require.Error(t, err)
	assert.Contains(t, err.Error(), expErr.Error())
}

func TestTracesPusher_maxMessageErr(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)

	p := kafkaTracesProducer{
		producer:  producer,
		marshaler: newPdataTracesMarshaler(&ptrace.ProtoMarshaler{}, defaultEncoding),
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 100}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	td := testdata.GenerateTracesTwoSpansSameResource()
	err := p.tracesPusher(context.Background(), td)
	assert.Contains(t, err.Error(), errSingleKafkaProducerMessageSizeOverMaxMsgByte.Error())
}

func TestTracesPusher_jaegerProto(t *testing.T) {
	c := sarama.NewConfig()

	tests := []struct {
		name                            string
		spanNum                         int
		maxMessageByte                  int
		mockProducerSuccessTimes        int
		singleSpanBigThenMaxMessageByte bool
		err                             error
	}{
		{
			name:                     "cut proto data ok",
			spanNum:                  2,
			mockProducerSuccessTimes: 2,
			maxMessageByte:           150,
			err:                      nil,
		}, {
			name:           "cut proto data err",
			spanNum:        2,
			maxMessageByte: 100,
			err:            errSingleKafkaProducerMessageSizeOverMaxMsgByte,
		},
	}

	for _, test := range tests {
		producer := mocks.NewSyncProducer(t, c)
		for i := 0; i < test.mockProducerSuccessTimes; i++ {
			producer.ExpectSendMessageAndSucceed()
		}

		p := kafkaTracesProducer{
			producer: producer,
			marshaler: jaegerMarshaler{
				marshaler: jaegerProtoSpanMarshaler{},
			},
			logger: zap.NewNop(),
			config: &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: test.maxMessageByte}},
		}

		t.Cleanup(func() {
			require.NoError(t, p.Close(context.Background()))
		})

		td := genJaegerTracesData(test.spanNum)

		assert.Equal(t, test.spanNum, td.SpanCount())
		batches, _ := jaeger.ProtoFromTraces(td)
		tdSize := 0
		for i := 0; i < len(batches); i++ {
			batches[i].Spans[0].Process = batches[i].Process
			jaegerProtoBytes, _ := batches[i].Spans[0].Marshal()
			require.NotNil(t, jaegerProtoBytes)
			messages := &sarama.ProducerMessage{
				Topic: "topic",
				Value: sarama.ByteEncoder(jaegerProtoBytes),
				Key:   sarama.ByteEncoder(batches[i].Spans[0].TraceID.String()),
			}
			// check singleSpanSize with maxMessageSize
			if messages.ByteSize(2) > test.maxMessageByte {
				test.singleSpanBigThenMaxMessageByte = true
			}

			tdSize += messages.ByteSize(2)
		}

		fmt.Println("current td size: ", tdSize)

		err := p.tracesPusher(context.Background(), td)
		if test.singleSpanBigThenMaxMessageByte {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), test.err.Error())
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestTracesPusher_jaegerJson(t *testing.T) {
	c := sarama.NewConfig()

	tests := []struct {
		name                            string
		spanNum                         int
		maxMessageByte                  int
		mockProducerSuccessTimes        int
		singleSpanBigThenMaxMessageByte bool
		err                             error
	}{
		{
			name:                     "cut proto data ok",
			spanNum:                  2,
			mockProducerSuccessTimes: 2,
			maxMessageByte:           800,
			err:                      nil,
		}, {
			name:           "cut proto data err",
			spanNum:        2,
			maxMessageByte: 100,
			err:            errSingleKafkaProducerMessageSizeOverMaxMsgByte,
		},
	}

	for _, test := range tests {
		producer := mocks.NewSyncProducer(t, c)
		for i := 0; i < test.mockProducerSuccessTimes; i++ {
			producer.ExpectSendMessageAndSucceed()
		}

		p := kafkaTracesProducer{
			producer: producer,
			marshaler: jaegerMarshaler{
				marshaler: jaegerJSONSpanMarshaler{
					pbMarshaler: &jsonpb.Marshaler{},
				},
			},
			logger: zap.NewNop(),
			config: &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: test.maxMessageByte}},
		}

		t.Cleanup(func() {
			require.NoError(t, p.Close(context.Background()))
		})

		td := genJaegerTracesData(test.spanNum)

		assert.Equal(t, test.spanNum, td.SpanCount())
		batches, _ := jaeger.ProtoFromTraces(td)
		tdSize := 0
		for i := 0; i < len(batches); i++ {
			batches[i].Spans[0].Process = batches[i].Process
			jsonMarshaler := &jsonpb.Marshaler{}
			jsonByteBuffer := new(bytes.Buffer)
			require.NoError(t, jsonMarshaler.Marshal(jsonByteBuffer, batches[0].Spans[0]))

			messages := &sarama.ProducerMessage{
				Topic: "topic",
				Value: sarama.ByteEncoder(jsonByteBuffer.Bytes()),
				Key:   sarama.ByteEncoder(batches[i].Spans[0].TraceID.String()),
			}
			// check singleSpanSize with maxMessageSize
			if messages.ByteSize(2) > test.maxMessageByte {
				test.singleSpanBigThenMaxMessageByte = true
			}

			tdSize += messages.ByteSize(2)
		}

		fmt.Println("current td size: ", tdSize)

		err := p.tracesPusher(context.Background(), td)
		if test.singleSpanBigThenMaxMessageByte {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), test.err.Error())
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestMetricsDataPusher(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)
	producer.ExpectSendMessageAndSucceed()

	p := kafkaMetricsProducer{
		producer:  producer,
		marshaler: newPdataMetricsMarshaler(&pmetric.ProtoMarshaler{}, defaultEncoding),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	err := p.metricsDataPusher(context.Background(), testdata.GenerateMetricsTwoMetrics())
	require.NoError(t, err)
}

func TestMetricsDataPusher_err(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)
	expErr := fmt.Errorf("failed to send")
	producer.ExpectSendMessageAndFail(expErr)

	p := kafkaMetricsProducer{
		producer:  producer,
		marshaler: newPdataMetricsMarshaler(&pmetric.ProtoMarshaler{}, defaultEncoding),
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	md := testdata.GenerateMetricsTwoMetrics()
	err := p.metricsDataPusher(context.Background(), md)
	assert.EqualError(t, err, expErr.Error())
}

func TestMetricsDataPusher_marshal_error(t *testing.T) {
	expErr := fmt.Errorf("failed to marshal")
	p := kafkaMetricsProducer{
		marshaler: &metricsErrorMarshaler{err: expErr},
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	md := testdata.GenerateMetricsTwoMetrics()
	err := p.metricsDataPusher(context.Background(), md)
	require.Error(t, err)
	assert.Contains(t, err.Error(), expErr.Error())
}

func TestMetricsPusher_maxMessageErr(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)

	p := kafkaMetricsProducer{
		producer:  producer,
		marshaler: newPdataMetricsMarshaler(&pmetric.ProtoMarshaler{}, defaultEncoding),
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 100}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	md := testdata.GenerateMetricsTwoMetrics()
	err := p.metricsDataPusher(context.Background(), md)
	assert.Contains(t, err.Error(), errSingleKafkaProducerMessageSizeOverMaxMsgByte.Error())
}

func TestLogsDataPusher(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)
	producer.ExpectSendMessageAndSucceed()

	p := kafkaLogsProducer{
		producer:  producer,
		marshaler: newPdataLogsMarshaler(&plog.ProtoMarshaler{}, defaultEncoding),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	err := p.logsDataPusher(context.Background(), testdata.GenerateLogsOneLogRecord())
	require.NoError(t, err)
}

func TestLogsDataPusher_err(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)
	expErr := fmt.Errorf("failed to send")
	producer.ExpectSendMessageAndFail(expErr)

	p := kafkaLogsProducer{
		producer:  producer,
		marshaler: newPdataLogsMarshaler(&plog.ProtoMarshaler{}, defaultEncoding),
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	ld := testdata.GenerateLogsOneLogRecord()
	err := p.logsDataPusher(context.Background(), ld)
	assert.EqualError(t, err, expErr.Error())
}

func TestLogsDataPusher_marshal_error(t *testing.T) {
	expErr := fmt.Errorf("failed to marshal")
	p := kafkaLogsProducer{
		marshaler: &logsErrorMarshaler{err: expErr},
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 1000 * 1000}},
	}
	ld := testdata.GenerateLogsOneLogRecord()
	err := p.logsDataPusher(context.Background(), ld)
	require.Error(t, err)
	assert.Contains(t, err.Error(), expErr.Error())
}

func TestLogsPusher_maxMessageErr(t *testing.T) {
	c := sarama.NewConfig()
	producer := mocks.NewSyncProducer(t, c)

	p := kafkaLogsProducer{
		producer:  producer,
		marshaler: newPdataLogsMarshaler(&plog.ProtoMarshaler{}, defaultEncoding),
		logger:    zap.NewNop(),
		config:    &Config{Producer: Producer{protoVersion: 2, MaxMessageBytes: 100}},
	}
	t.Cleanup(func() {
		require.NoError(t, p.Close(context.Background()))
	})
	ld := testdata.GenerateLogsTwoLogRecordsSameResource()
	err := p.logsDataPusher(context.Background(), ld)
	assert.Contains(t, err.Error(), errSingleKafkaProducerMessageSizeOverMaxMsgByte.Error())
}

type tracesErrorMarshaler struct {
	err error
}

type metricsErrorMarshaler struct {
	err error
}

type logsErrorMarshaler struct {
	err error
}

func (e metricsErrorMarshaler) Marshal(_ pmetric.Metrics, _ *Config) ([]*sarama.ProducerMessage, error) {
	return nil, e.err
}

func (e metricsErrorMarshaler) Encoding() string {
	panic("implement me")
}

var _ TracesMarshaler = (*tracesErrorMarshaler)(nil)

func (e tracesErrorMarshaler) Marshal(_ ptrace.Traces, _ *Config) ([]*sarama.ProducerMessage, error) {
	return nil, e.err
}

func (e tracesErrorMarshaler) Encoding() string {
	panic("implement me")
}

func (e logsErrorMarshaler) Marshal(_ plog.Logs, _ *Config) ([]*sarama.ProducerMessage, error) {
	return nil, e.err
}

func (e logsErrorMarshaler) Encoding() string {
	panic("implement me")
}
