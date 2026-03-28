import { useEffect } from "react";
import { useQuery } from "@apollo/client/react";
import { gql } from "@apollo/client/core";
import { Link, Navigate, useNavigate, useParams } from "react-router-dom";
import { ProgressBar } from "../components/ProgressBar";

const JOB_QUERY = gql`
  query Job($id: ID!) {
    job(id: $id) {
      id
      status
      totalCount
      processedCount
      successCount
      failureCount
      externalJobId
      statusMessage
      errors {
        id
        rowNumber
        name
        email
        message
      }
    }
  }
`;

type JobStatus = "QUEUED" | "RUNNING" | "COMPLETED" | "FAILED";

type JobQueryData = {
  job: {
    id: string;
    status: JobStatus;
    totalCount: number;
    processedCount: number;
    successCount: number;
    failureCount: number;
    externalJobId?: string | null;
    statusMessage?: string | null;
    errors: Array<{
      id: string;
      rowNumber: number;
      name: string;
      email: string;
      message: string;
    }>;
  } | null;
};

type JobQueryVariables = {
  id: string;
};

export function ProcessingPage() {
  const { jobId } = useParams();
  const navigate = useNavigate();

  const { data, loading, error } = useQuery<JobQueryData, JobQueryVariables>(JOB_QUERY, {
    variables: { id: jobId ?? "" },
    skip: !jobId,
    pollInterval: 2000,
    fetchPolicy: "network-only",
  });

  const job = data?.job;
  const isTerminal = job?.status === "COMPLETED" || job?.status === "FAILED";

  useEffect(() => {
    if (jobId && isTerminal) {
      navigate(`/jobs/${jobId}/complete`, { replace: true });
    }
  }, [isTerminal, jobId, navigate]);

  if (!jobId) {
    return <Navigate to="/" replace />;
  }

  if (!loading && !job && !error) {
    return (
      <main className="page-shell">
        <section className="panel empty-panel">
          <p className="eyebrow">Job</p>
          <h2>ジョブが見つかりません</h2>
          <p className="helper-text">指定されたジョブIDは存在しません。</p>
          <Link className="text-link" to="/">
            作成画面へ戻る
          </Link>
        </section>
      </main>
    );
  }

  return (
    <main className="page-shell">
      <section className="hero-panel">
        <div className="hero-copy">
          <p className="eyebrow">Processing</p>
          <h1>作成処理を実行中</h1>
          <p className="lead">
            2秒ごとに進捗を取得しています。完了すると自動で結果画面へ遷移します。
          </p>
        </div>
        <div className="status-card">
          <p className="status-label">Job ID</p>
          <p className="status">{jobId}</p>
          <p className={`status ${error ? "error" : "ok"}`}>
            {error ? "取得エラー" : loading && !job ? "読み込み中..." : statusLabel[job?.status ?? "QUEUED"]}
          </p>
          {job?.externalJobId ? <p className="status-note">Provider Job: {job.externalJobId}</p> : null}
        </div>
      </section>

      <section className="panel">
        <div className="section-heading">
          <div>
            <p className="eyebrow">Progress</p>
            <h2>進捗</h2>
          </div>
        </div>
        {job ? (
          <>
            <ProgressBar value={job.processedCount} max={job.totalCount} />
            <div className="summary-grid">
              <article className="summary-card">
                <span>成功</span>
                <strong>{job.successCount}件</strong>
              </article>
              <article className="summary-card">
                <span>失敗</span>
                <strong>{job.failureCount}件</strong>
              </article>
              <article className="summary-card">
                <span>残り</span>
                <strong>{Math.max(job.totalCount - job.processedCount, 0)}件</strong>
              </article>
            </div>
            {job.errors.length > 0 ? (
              <p className="status error">
                現在 {job.errors.length} 件のエラーがあります。完了画面で詳細を確認できます。
              </p>
            ) : null}
            {job.statusMessage ? <p className="helper-text">{job.statusMessage}</p> : null}
          </>
        ) : null}
        {error ? <p className="status error">{error.message}</p> : null}
      </section>
    </main>
  );
}

const statusLabel: Record<JobStatus, string> = {
  QUEUED: "待機中",
  RUNNING: "実行中",
  COMPLETED: "完了",
  FAILED: "失敗",
};
