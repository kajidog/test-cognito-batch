import { useQuery } from "@apollo/client/react";
import { gql } from "@apollo/client/core";
import { Link, Navigate, useParams } from "react-router-dom";

const JOB_QUERY = gql`
  query Job($id: ID!) {
    job(id: $id) {
      id
      status
      totalCount
      processedCount
      successCount
      failureCount
      sourceObjectKey
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
    sourceObjectKey?: string | null;
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

export function CompletionPage() {
  const { jobId } = useParams();
  const { data, loading, error } = useQuery<JobQueryData, JobQueryVariables>(JOB_QUERY, {
    variables: { id: jobId ?? "" },
    skip: !jobId,
    fetchPolicy: "network-only",
  });

  if (!jobId) {
    return <Navigate to="/" replace />;
  }

  const job = data?.job;

  return (
    <main className="page-shell">
      <section className="hero-panel">
        <div className="hero-copy">
          <p className="eyebrow">Completed</p>
          <h1>一括登録の結果</h1>
          <p className="lead">
            ジョブ完了後の成功件数・失敗件数と、失敗した行の詳細を確認できます。
          </p>
        </div>
        <div className="status-card">
          <p className="status-label">Job ID</p>
          <p className="status">{jobId}</p>
          <p className={`status ${job?.status === "FAILED" || error ? "error" : "ok"}`}>
            {loading ? "読み込み中..." : error ? "取得エラー" : statusLabel[job?.status ?? "COMPLETED"]}
          </p>
          {job?.sourceObjectKey ? (
            <p className="status-note">S3 Object: {job.sourceObjectKey}</p>
          ) : null}
        </div>
      </section>

      {job ? (
        <>
          <section className="panel">
            <div className="section-heading">
              <div>
                <p className="eyebrow">Summary</p>
                <h2>処理結果サマリー</h2>
              </div>
              <Link className="text-link" to="/">
                新しいCSVを登録
              </Link>
            </div>
            <div className="summary-grid">
              <article className="summary-card">
                <span>総件数</span>
                <strong>{job.totalCount}件</strong>
              </article>
              <article className="summary-card summary-card-new">
                <span>成功</span>
                <strong>{job.successCount}件</strong>
              </article>
              <article className="summary-card summary-card-error">
                <span>失敗</span>
                <strong>{job.failureCount}件</strong>
              </article>
            </div>
          </section>

          <section className="panel">
            <div className="section-heading">
              <div>
                <p className="eyebrow">Errors</p>
                <h2>失敗行</h2>
              </div>
            </div>
            {job.errors.length > 0 ? (
              <div className="table-scroll">
                <table className="user-table">
                  <thead>
                    <tr>
                      <th>行</th>
                      <th>名前</th>
                      <th>メールアドレス</th>
                      <th>エラー内容</th>
                    </tr>
                  </thead>
                  <tbody>
                    {job.errors.map((jobError) => (
                      <tr key={jobError.id} className="row-error">
                        <td>{jobError.rowNumber || "-"}</td>
                        <td>{jobError.name || "-"}</td>
                        <td>{jobError.email || "-"}</td>
                        <td className="cell-error">
                          <div className="cell-stack">
                            <span>{jobError.message}</span>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p className="status ok">失敗行はありません。</p>
            )}
            {error ? <p className="status error">{error.message}</p> : null}
          </section>
        </>
      ) : (
        <section className="panel empty-panel">
          <p className="eyebrow">Job</p>
          <h2>ジョブが見つかりません</h2>
          <p className="helper-text">指定されたジョブIDは存在しません。</p>
        </section>
      )}
    </main>
  );
}

const statusLabel: Record<JobStatus, string> = {
  QUEUED: "待機中",
  RUNNING: "実行中",
  COMPLETED: "完了",
  FAILED: "失敗",
};
