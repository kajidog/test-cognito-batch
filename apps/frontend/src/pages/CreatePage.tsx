import { useState } from "react";
import { useLazyQuery, useMutation, useQuery } from "@apollo/client/react";
import { gql } from "@apollo/client/core";
import Papa from "papaparse";
import { useNavigate } from "react-router-dom";
import { CsvUploader } from "../components/CsvUploader";
import {
  UserTable,
  type ParsedUserRow,
  type RowValidation,
  type ValidationSummary,
} from "../components/UserTable";

const HEALTH_QUERY = gql`
  query Health {
    health
  }
`;

const VALIDATE_USERS_QUERY = gql`
  query ValidateUsers($inputs: [UserInput!]!) {
    validateUsers(inputs: $inputs) {
      summary {
        newCount
        updateCount
        errorCount
      }
      rows {
        rowNumber
        status
        errors {
          field
          message
        }
      }
    }
  }
`;

const START_BATCH_UPSERT_MUTATION = gql`
  mutation StartBatchUpsert($inputs: [UserInput!]!) {
    startBatchUpsert(inputs: $inputs) {
      id
      status
      totalCount
      processedCount
      successCount
      failureCount
    }
  }
`;

type HealthQueryData = {
  health: string;
};

type ValidateUsersQueryData = {
  validateUsers: {
    summary: ValidationSummary;
    rows: RowValidation[];
  };
};

type ValidateUsersResult = ValidateUsersQueryData["validateUsers"];

type ValidateUsersQueryVariables = {
  inputs: Array<{
    email: string;
    username: string;
    name: string;
    cognitoId?: string | null;
  }>;
};

type StartBatchUpsertMutationData = {
  startBatchUpsert: {
    id: string;
  };
};

type StartBatchUpsertMutationVariables = ValidateUsersQueryVariables;

const HEADER_ALIASES: Record<string, keyof Omit<ParsedUserRow, "rowNumber">> = {
  email: "email",
  mail: "email",
  "メール": "email",
  "メールアドレス": "email",
  username: "username",
  user_name: "username",
  "user id": "username",
  user_id: "username",
  "ユーザー名": "username",
  name: "name",
  "名前": "name",
  cognitoid: "cognitoId",
  cognito_id: "cognitoId",
  "cognito id": "cognitoId",
};

export function CreatePage() {
  const navigate = useNavigate();
  const { data, loading, error } = useQuery<HealthQueryData>(HEALTH_QUERY);
  const [rows, setRows] = useState<ParsedUserRow[]>([]);
  const [fileName, setFileName] = useState("");
  const [parseError, setParseError] = useState("");
  const [validationMessage, setValidationMessage] = useState("");
  const [validationResult, setValidationResult] = useState<ValidateUsersResult | null>(null);
  const [runValidation, { loading: validationLoading, error: validationError }] =
    useLazyQuery<ValidateUsersQueryData, ValidateUsersQueryVariables>(VALIDATE_USERS_QUERY, {
      fetchPolicy: "no-cache",
    });
  const [startBatchUpsert, { loading: startLoading, error: startError }] = useMutation<
    StartBatchUpsertMutationData,
    StartBatchUpsertMutationVariables
  >(START_BATCH_UPSERT_MUTATION);

  const handleFileSelected = (file: File) => {
    setFileName(file.name);
    setParseError("");
    setValidationMessage("");
    setValidationResult(null);

    Papa.parse<Record<string, string>>(file, {
      header: true,
      skipEmptyLines: true,
      complete: (result) => {
        if (result.errors.length > 0) {
          setRows([]);
          setParseError(result.errors[0].message);
          setValidationResult(null);
          return;
        }

        const normalizedRows = result.data
          .map((row, index) => normalizeRow(row, index))
          .filter((row) => row.email || row.username || row.name || row.cognitoId);

        setRows(normalizedRows);
      },
      error: (parseError) => {
        setRows([]);
        setParseError(parseError.message);
        setValidationResult(null);
      },
    });
  };

  const handleValidate = async () => {
    if (rows.length === 0) {
      return;
    }

    setValidationMessage("");

    const result = await runValidation({
      variables: {
        inputs: rows.map((row) => ({
          email: row.email,
          username: row.username,
          name: row.name,
          cognitoId: row.cognitoId || null,
        })),
      },
    });

    const summary = result.data?.validateUsers.summary;
    const validateUsers = result.data?.validateUsers;
    if (!summary || !validateUsers) {
      return;
    }

    setValidationResult(validateUsers);
    setValidationMessage(
      `新規 ${summary.newCount} 件 / 更新 ${summary.updateCount} 件 / エラー ${summary.errorCount} 件`,
    );
  };

  const handleStart = async () => {
    if (rows.length === 0 || !validationResult || startLoading) {
      return;
    }

    const result = await startBatchUpsert({
      variables: {
        inputs: rows.map((row) => ({
          email: row.email,
          username: row.username,
          name: row.name,
          cognitoId: row.cognitoId || null,
        })),
      },
    });

    const jobId = result.data?.startBatchUpsert.id;
    if (!jobId) {
      return;
    }

    navigate(`/jobs/${jobId}`);
  };

  const canStart = rows.length > 0 && validationResult !== null && !validationLoading && !startLoading;

  return (
    <main className="page-shell">
      <section className="hero-panel">
        <div className="hero-copy">
          <p className="eyebrow">Phase 5</p>
          <h1>Cognito 一括登録</h1>
          <p className="lead">
            CSV を読み込んで検証し、そのまま一括登録ジョブを開始できます。
          </p>
        </div>
        <div className="status-card">
          <p className="status-label">Backend</p>
          <p className={`status ${error ? "error" : "ok"}`}>
            {loading ? "接続中..." : error ? "接続エラー" : data?.health}
          </p>
          <p className="status-note">
            {fileName ? `選択中: ${fileName}` : "CSV を選ぶとプレビューが表示されます。"}
          </p>
        </div>
      </section>

      <CsvUploader onFileSelected={handleFileSelected} errorMessage={parseError} />

      {rows.length > 0 ? (
        <>
          <section className="panel">
            <div className="section-heading">
              <div>
                <p className="eyebrow">Validation</p>
                <h2>バックエンド検証</h2>
              </div>
              <button
                type="button"
                className="action-button"
                onClick={handleValidate}
                disabled={validationLoading || startLoading}
              >
                {validationLoading ? "チェック中..." : "チェック"}
              </button>
            </div>
              <p className="helper-text">
              メール形式、username の形式、名前の文字数、CSV 内重複、既存ユーザーとの突合を行います。
            </p>
            {validationMessage ? <p className="status ok">{validationMessage}</p> : null}
            {validationError ? (
              <p className="status error">{validationError.message}</p>
            ) : null}
            <div className="action-row">
              <button
                type="button"
                className="action-button action-button-secondary"
                onClick={handleStart}
                disabled={!canStart}
              >
                {startLoading ? "登録開始中..." : "登録を開始"}
              </button>
              <p className="helper-text">
                検証結果を確認後、ジョブを作成して処理中画面へ遷移します。
              </p>
            </div>
            {startError ? <p className="status error">{startError.message}</p> : null}
          </section>
          <UserTable
            rows={rows}
            validationRows={validationResult?.rows}
            summary={validationResult?.summary}
          />
        </>
      ) : (
        <section className="panel empty-panel">
          <p className="eyebrow">Preview</p>
          <h2>CSVプレビュー待機中</h2>
          <p className="helper-text">
            CSV を読み込むと、ここにテーブル形式で内容を表示します。
          </p>
        </section>
      )}
    </main>
  );
}

function normalizeRow(
  rawRow: Record<string, string>,
  index: number,
): ParsedUserRow {
  const normalized: ParsedUserRow = {
    rowNumber: index + 2,
    email: "",
    username: "",
    name: "",
    cognitoId: "",
  };

  Object.entries(rawRow).forEach(([key, value]) => {
    const alias = HEADER_ALIASES[key.trim().toLowerCase()];
    if (!alias) {
      return;
    }
    normalized[alias] = value?.trim() ?? "";
  });

  return normalized;
}
