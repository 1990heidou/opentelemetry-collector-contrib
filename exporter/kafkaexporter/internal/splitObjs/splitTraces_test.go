package splitObjs

import (
	"fmt"
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/kafkaexporter"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/kafkaexporter/internal/testdata"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func cutTracesByMaxSpanCount(splitSize int, td ptrace.Traces, maxSpanCount int) (dest []ptrace.Traces) {
	// 因为一旦切割粒度不小于当前拥有的总数量，则切割完毕后，切前的对象和前后的对象一样
	// 因此这样就会产生问题，这里直接将切割粒度/2
	if kafkaexporter.tracesSpansNum(td) <= splitSize {
		return cutTracesByMaxSpanCount(splitSize/2, td, maxSpanCount)
	}
	// 进行分割
	split := SplitTraces(splitSize, td)

	// 判断切下的那边是否需要再切，此时切割粒度/2
	if kafkaexporter.tracesSpansNum(split) > maxSpanCount {
		left := cutTracesByMaxSpanCount(splitSize/2, split, maxSpanCount)
		dest = append(dest, left...)
	} else {
		dest = append(dest, split)
	}

	// 判断切剩的那边是否还需要再切，此时切割粒度保持
	if kafkaexporter.tracesSpansNum(td) > maxSpanCount {
		right := cutTracesByMaxSpanCount(splitSize, td, maxSpanCount)
		dest = append(dest, right...)
	} else {
		dest = append(dest, td)
	}
	return dest
}

func TestSplitTraces_maxSpanCount(t *testing.T) {
	totalSpanCount := 20
	cutSpansCount := 0
	td := testdata.GenerateTraces(totalSpanCount)
	fmt.Println("num: ", kafkaexporter.tracesSpansNum(td))
	split := cutTracesByMaxSpanCount(5, td, 2)
	for _, s := range split {
		cutSpansCount += s.ResourceSpans().At(0).ScopeSpans().At(0).Spans().Len()
	}
	assert.Equal(t, totalSpanCount, cutSpansCount)
}
