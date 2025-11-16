// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/milvus-io/milvus/pkg/v2/proto/querypb"
	"github.com/milvus-io/milvus/pkg/v2/proto/internalpb"
)

// InitTracing initializes OpenTelemetry tracing
func InitTracing(serviceName string, jaegerEndpoint string) (func(), error) {
	// Create Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(
		jaeger.WithEndpoint(jaegerEndpoint),
	))
	if err != nil {
		return nil, fmt.Errorf("failed to create Jaeger exporter: %w", err)
	}

	// Create trace provider
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("component", "querynode"),
		)),
		// Sample 100% of traces (configurable in production)
		trace.WithSampler(trace.AlwaysSample()),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Return cleanup function
	cleanup := func() {
		_ = tp.Shutdown(context.Background())
	}

	return cleanup, nil
}

// SearchTracer wraps search operations with tracing
type SearchTracer struct {
	tracer oteltrace.Tracer
}

func NewSearchTracer() *SearchTracer {
	return &SearchTracer{
		tracer: otel.Tracer("milvus.querynode.search"),
	}
}

// TraceSearch wraps entire search operation
func (t *SearchTracer) TraceSearch(
	ctx context.Context,
	req *querypb.SearchRequest,
	fn func(context.Context) (*internalpb.SearchResults, error),
) (*internalpb.SearchResults, error) {
	ctx, span := t.tracer.Start(ctx, "search_request",
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
	)
	defer span.End()

	// Add request attributes
	span.SetAttributes(
		attribute.Int64("collection_id", req.GetReq().GetCollectionID()),
		attribute.Int64("num_segments", int64(len(req.GetSegmentIDs()))),
		attribute.String("index_type", getIndexType(req)),
		attribute.Int64("top_k", int64(req.GetReq().GetTopk())),
		attribute.Int64("nq", int64(req.GetReq().GetNq())),
		attribute.Int("dsl_type", int(req.GetReq().GetDslType())),
	)

	// Execute search
	results, err := fn(ctx)

	// Record error if any
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	} else {
		span.SetAttributes(
			attribute.Int64("num_results", int64(len(results.GetSlicedBlob()))),
			attribute.Bool("success", true),
		)
	}

	return results, err
}

// TraceDelegatorSearch traces delegator-level search
func (t *SearchTracer) TraceDelegatorSearch(
	ctx context.Context,
	channel string,
	fn func(context.Context) (*internalpb.SearchResults, error),
) (*internalpb.SearchResults, error) {
	ctx, span := t.tracer.Start(ctx, "delegator_search")
	defer span.End()

	span.SetAttributes(
		attribute.String("channel", channel),
	)

	results, err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	}

	return results, err
}

// TraceSegmentSearch traces individual segment search
func (t *SearchTracer) TraceSegmentSearch(
	ctx context.Context,
	segmentID int64,
	indexType string,
	numVectors int64,
	fn func(context.Context) error,
) error {
	ctx, span := t.tracer.Start(ctx, "search_segment")
	defer span.End()

	span.SetAttributes(
		attribute.Int64("segment_id", segmentID),
		attribute.String("index_type", indexType),
		attribute.Int64("num_vectors", numVectors),
	)

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	}

	return err
}

// TraceMergeResults traces result merging
func (t *SearchTracer) TraceMergeResults(
	ctx context.Context,
	numResults int,
	fn func(context.Context) error,
) error {
	ctx, span := t.tracer.Start(ctx, "merge_results")
	defer span.End()

	span.SetAttributes(
		attribute.Int("num_partial_results", numResults),
	)

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	}

	return err
}

// TracePlanQuery traces query planning stage
func (t *SearchTracer) TracePlanQuery(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	ctx, span := t.tracer.Start(ctx, "plan_query")
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	}

	return err
}

// TraceParseRequest traces request parsing stage
func (t *SearchTracer) TraceParseRequest(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	ctx, span := t.tracer.Start(ctx, "parse_request")
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("error", true))
	}

	return err
}

func getIndexType(req *querypb.SearchRequest) string {
	// Extract index type from request
	// This is simplified - actual implementation would query metadata
	// For now, return a placeholder
	return "HNSW"
}
