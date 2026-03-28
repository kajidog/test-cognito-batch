type CsvUploaderProps = {
  onFileSelected: (file: File) => void;
  errorMessage?: string;
};

export function CsvUploader({
  onFileSelected,
  errorMessage,
}: CsvUploaderProps) {
  return (
    <section className="panel">
      <div className="section-heading">
        <p className="eyebrow">CSV Upload</p>
        <h2>ユーザーCSVを読み込む</h2>
      </div>
      <label className="upload-field">
        <span>CSVファイルを選択</span>
        <input
          type="file"
          accept=".csv,text/csv"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (!file) {
              return;
            }
            onFileSelected(file);
          }}
        />
      </label>
      <p className="helper-text">
        1行目はヘッダーとして扱います。`email`, `name`, `cognitoId` を認識します。
      </p>
      {errorMessage ? <p className="status error">{errorMessage}</p> : null}
    </section>
  );
}
