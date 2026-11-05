# Open Ontologies — public web export history

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

閲覧用ウェブコンテンツの完成したスナップショットを `content/` で追跡します。
追加・変更・削除を記録し、同じ内容の出力ではコミットを作りません。
GitHub remote・自動push・実行スケジュールは設定していません。

公開先: https://open-ontologies.moukaeritai.work/ontologies/

## 生成と公開

生成/検証/記録/公開コードは独立した `open-ontologies-site` リポジトリで管理します。
このホストでは次のコマンドを手動実行します。

```sh
/data/service-builds/open-ontologies-web-export-venv/bin/python \
  /data/open-ontologies-site/main/scripts/export_web.py \
  --config /home/ubuntu/mcp-services/open-ontologies/export-web.json \
  --report /data/service-builds/open-ontologies-web-export-report.json
```

サイトのREADMEに設定、検証、失敗時の扱い、切り戻し方法を記載しています。
CaddyはGitディレクトリを直接公開せず、検証済みコミットから作った配信コピーを公開します。

## 完成済み出力を記録するだけの場合

```sh
python3 /data/open-ontologies-exports/main/scripts/record-export.py /absolute/path/to/completed-export
```

これは保守元のサイトリポジトリの記録コマンドを呼ぶホスト用ラッパーです。
HTML生成・検証・公開は行いません。手動で使う場合は事前に出力を検証してください。
取り込み元は処理中に変更せず、ルートに `index.html` を置いてください。
未コミット変更がある場合は停止します。コミット失敗時に残る変更は手動で確認してください。

## 内容・権利と履歴の範囲

公開指定されたRDFと閲覧ページだけを保存します。DB・設定・秘密情報は含めません。
サイトコード/表示構成のMITライセンスは `content/assets/LICENSE.txt` に収録されます。
RDFデータは各出典の権利・利用条件に従い、そのMITライセンスの一律適用対象ではありません。
元データ中の出典・ライセンス述語を保持します。

この履歴は完成したエクスポートごとのスナップショットです。
実行間隔の途中のすべての更新を記録する操作ログではありません。
取得は初版では選択グラフの全件読み取りで、ネイティブ変更フィードは未実装です。
