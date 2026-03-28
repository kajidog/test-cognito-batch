export type ParsedUserRow = {
  rowNumber: number;
  email: string;
  name: string;
  cognitoId: string;
};

export type ValidationStatus = "NEW" | "UPDATE" | "ERROR";

export type FieldValidationError = {
  field: string;
  message: string;
};

export type RowValidation = {
  rowNumber: number;
  status: ValidationStatus;
  errors: FieldValidationError[];
};

export type ValidationSummary = {
  newCount: number;
  updateCount: number;
  errorCount: number;
};

type UserTableProps = {
  rows: ParsedUserRow[];
  validationRows?: RowValidation[];
  summary?: ValidationSummary;
};

export function UserTable({
  rows,
  validationRows = [],
  summary,
}: UserTableProps) {
  const validationByRowNumber = new Map(
    validationRows.map((row) => [row.rowNumber, row] as const),
  );

  return (
    <section className="panel">
      <div className="section-heading">
        <p className="eyebrow">Preview</p>
        <h2>CSVプレビュー</h2>
      </div>
      {summary ? (
        <div className="summary-grid">
          <article className="summary-card summary-card-new">
            <span>新規</span>
            <strong>{summary.newCount}件</strong>
          </article>
          <article className="summary-card summary-card-update">
            <span>更新</span>
            <strong>{summary.updateCount}件</strong>
          </article>
          <article className="summary-card summary-card-error">
            <span>エラー</span>
            <strong>{summary.errorCount}件</strong>
          </article>
        </div>
      ) : null}
      <div className="table-scroll">
        <table className="user-table">
          <thead>
            <tr>
              <th>行</th>
              <th>判定</th>
              <th>メールアドレス</th>
              <th>名前</th>
              <th>Cognito ID</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const validation = validationByRowNumber.get(row.rowNumber);
              const fieldErrors = new Map(
                (validation?.errors ?? []).map((error) => [error.field, error.message] as const),
              );

              return (
                <tr
                  key={`${row.rowNumber}-${row.name}`}
                  className={validation ? `row-${validation.status.toLowerCase()}` : undefined}
                >
                  <td>{row.rowNumber}</td>
                  <td>
                    {validation ? (
                      <span
                        className={`status-pill status-pill-${validation.status.toLowerCase()}`}
                      >
                        {statusLabel[validation.status]}
                      </span>
                    ) : (
                      "-"
                    )}
                  </td>
                  <td className={fieldErrors.has("email") ? "cell-error" : undefined}>
                    <CellContent value={row.email} error={fieldErrors.get("email")} />
                  </td>
                  <td className={fieldErrors.has("name") ? "cell-error" : undefined}>
                    <CellContent value={row.name} error={fieldErrors.get("name")} />
                  </td>
                  <td>
                    <CellContent value={row.cognitoId} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

const statusLabel: Record<ValidationStatus, string> = {
  NEW: "新規",
  UPDATE: "更新",
  ERROR: "エラー",
};

type CellContentProps = {
  value: string;
  error?: string;
};

function CellContent({ value, error }: CellContentProps) {
  return (
    <div className="cell-stack">
      <span>{value || "-"}</span>
      {error ? <span className="cell-error-text">{error}</span> : null}
    </div>
  );
}
