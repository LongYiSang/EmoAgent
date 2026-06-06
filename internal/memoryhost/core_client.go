package memoryhost

import (
	"context"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type CoreClient interface {
	Close() error

	StartSession(context.Context, memorycore.StartSessionRequest) (*memorycore.Session, error)
	EndSession(context.Context, memorycore.EndSessionRequest) (*memorycore.Session, error)
	AppendEpisode(context.Context, memorycore.AppendEpisodeRequest) (*memorycore.Episode, error)

	Retrieve(context.Context, memorycore.RetrievalRequest) (*memorycore.MemoryContext, error)

	EnsureEntity(context.Context, memorycore.EnsureEntityRequest) (*memorycore.Entity, error)
	ConsolidateCandidate(context.Context, memorycore.ConsolidateCandidateRequest) (*memorycore.ConsolidationResult, error)
	RunExtraction(context.Context, memorycore.RunExtractionRequest) (*memorycore.ExtractionRunResult, error)
	RunExtractionBatch(context.Context, memorycore.ExtractionBatchRequest) (*memorycore.ExtractionBatchResult, error)

	PreviewForget(context.Context, memorycore.ForgetPreviewRequest) (*memorycore.ForgetPreviewResult, error)
	ExecuteForget(context.Context, memorycore.ForgetExecuteRequest) (*memorycore.ForgetExecuteResult, error)
	VerifyForget(context.Context, memorycore.ForgetVerifyRequest) (*memorycore.ForgetVerifyResult, error)
	GetPendingManualForgetOperation(context.Context, memorycore.GetPendingManualForgetOperationRequest) (*memorycore.PendingManualForgetOperation, error)
	CancelPendingManualForgetOperation(context.Context, memorycore.CancelPendingManualForgetOperationRequest) (*memorycore.PendingManualForgetOperation, error)

	RunMirrorSync(context.Context, memorycore.RunMirrorSyncRequest) (*memorycore.RunMirrorSyncResult, error)
	RebuildMirror(context.Context, memorycore.RebuildMirrorRequest) (*memorycore.RebuildMirrorResult, error)
	RebuildSearchDocuments(context.Context, memorycore.RebuildSearchDocumentsRequest) (*memorycore.RebuildSearchDocumentsResult, error)
	RunRetentionJobs(context.Context, memorycore.RunRetentionJobsRequest) (*memorycore.RunRetentionJobsResult, error)
	RunNaturalMemoryCycle(context.Context, memorycore.RunNaturalMemoryCycleRequest) (*memorycore.RunNaturalMemoryCycleResult, error)
	RunNaturalMemoryTick(context.Context, memorycore.RunNaturalMemoryTickRequest) (*memorycore.RunNaturalMemoryCycleResult, error)
}

type memoryCoreClientAdapter struct {
	client *memorycore.Client
}

func newMemoryCoreClientAdapter(client *memorycore.Client) CoreClient {
	if client == nil {
		return nil
	}
	return memoryCoreClientAdapter{client: client}
}

func (a memoryCoreClientAdapter) Close() error {
	return a.client.Close()
}

func (a memoryCoreClientAdapter) StartSession(ctx context.Context, req memorycore.StartSessionRequest) (*memorycore.Session, error) {
	return a.client.Sessions().StartSession(ctx, req)
}

func (a memoryCoreClientAdapter) EndSession(ctx context.Context, req memorycore.EndSessionRequest) (*memorycore.Session, error) {
	return a.client.Sessions().EndSession(ctx, req)
}

func (a memoryCoreClientAdapter) AppendEpisode(ctx context.Context, req memorycore.AppendEpisodeRequest) (*memorycore.Episode, error) {
	return a.client.Sessions().AppendEpisode(ctx, req)
}

func (a memoryCoreClientAdapter) Retrieve(ctx context.Context, req memorycore.RetrievalRequest) (*memorycore.MemoryContext, error) {
	return a.client.Retrieval().Retrieve(ctx, req)
}

func (a memoryCoreClientAdapter) EnsureEntity(ctx context.Context, req memorycore.EnsureEntityRequest) (*memorycore.Entity, error) {
	return a.client.Writes().EnsureEntity(ctx, req)
}

func (a memoryCoreClientAdapter) ConsolidateCandidate(ctx context.Context, req memorycore.ConsolidateCandidateRequest) (*memorycore.ConsolidationResult, error) {
	return a.client.Writes().ConsolidateCandidate(ctx, req)
}

func (a memoryCoreClientAdapter) RunExtraction(ctx context.Context, req memorycore.RunExtractionRequest) (*memorycore.ExtractionRunResult, error) {
	return a.client.Writes().RunExtraction(ctx, req)
}

func (a memoryCoreClientAdapter) RunExtractionBatch(ctx context.Context, req memorycore.ExtractionBatchRequest) (*memorycore.ExtractionBatchResult, error) {
	return a.client.Writes().RunExtractionBatch(ctx, req)
}

func (a memoryCoreClientAdapter) PreviewForget(ctx context.Context, req memorycore.ForgetPreviewRequest) (*memorycore.ForgetPreviewResult, error) {
	return a.client.Forget().PreviewForget(ctx, req)
}

func (a memoryCoreClientAdapter) ExecuteForget(ctx context.Context, req memorycore.ForgetExecuteRequest) (*memorycore.ForgetExecuteResult, error) {
	return a.client.Forget().ExecuteForget(ctx, req)
}

func (a memoryCoreClientAdapter) VerifyForget(ctx context.Context, req memorycore.ForgetVerifyRequest) (*memorycore.ForgetVerifyResult, error) {
	return a.client.Forget().VerifyForget(ctx, req)
}

func (a memoryCoreClientAdapter) GetPendingManualForgetOperation(ctx context.Context, req memorycore.GetPendingManualForgetOperationRequest) (*memorycore.PendingManualForgetOperation, error) {
	return a.client.Forget().GetPendingManualForgetOperation(ctx, req)
}

func (a memoryCoreClientAdapter) CancelPendingManualForgetOperation(ctx context.Context, req memorycore.CancelPendingManualForgetOperationRequest) (*memorycore.PendingManualForgetOperation, error) {
	return a.client.Forget().CancelPendingManualForgetOperation(ctx, req)
}

func (a memoryCoreClientAdapter) RunMirrorSync(ctx context.Context, req memorycore.RunMirrorSyncRequest) (*memorycore.RunMirrorSyncResult, error) {
	return a.client.Ops().RunMirrorSync(ctx, req)
}

func (a memoryCoreClientAdapter) RebuildMirror(ctx context.Context, req memorycore.RebuildMirrorRequest) (*memorycore.RebuildMirrorResult, error) {
	return a.client.Ops().RebuildMirror(ctx, req)
}

func (a memoryCoreClientAdapter) RebuildSearchDocuments(ctx context.Context, req memorycore.RebuildSearchDocumentsRequest) (*memorycore.RebuildSearchDocumentsResult, error) {
	return a.client.Ops().RebuildSearchDocuments(ctx, req)
}

func (a memoryCoreClientAdapter) RunRetentionJobs(ctx context.Context, req memorycore.RunRetentionJobsRequest) (*memorycore.RunRetentionJobsResult, error) {
	return a.client.Ops().RunRetentionJobs(ctx, req)
}

func (a memoryCoreClientAdapter) RunNaturalMemoryCycle(ctx context.Context, req memorycore.RunNaturalMemoryCycleRequest) (*memorycore.RunNaturalMemoryCycleResult, error) {
	return a.client.Ops().RunNaturalMemoryCycle(ctx, req)
}

func (a memoryCoreClientAdapter) RunNaturalMemoryTick(ctx context.Context, req memorycore.RunNaturalMemoryTickRequest) (*memorycore.RunNaturalMemoryCycleResult, error) {
	return a.client.Ops().RunNaturalMemoryTick(ctx, req)
}
