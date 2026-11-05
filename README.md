# Open Ontologies — public web export history

Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)

完成した閲覧用ウェブコンテンツをローカルGitで記録するための独立リポジトリです。
現段階では履歴記録の仕組みだけを用意しています。実データのウェブ生成、
Caddyでの公開、定期実行、GitHubへのpushはまだ設定していません。

## 完成した出力の記録

生成処理が成功した後、公開対象を確認した出力ディレクトリを渡します。
ルートには `index.html` が必要です。

```sh
python3 /data/open-ontologies-exports/main/scripts/record-export.py /absolute/path/to/completed-export
```

スクリプトは出力一式を `content/` に取り込み、追加・更新・削除をステージし、
変更がある場合だけコミットします。同一内容ではコミットしません。
生成元は取り込み中に変更しないでください。並行取り込みはロックで直列化します。
未コミット変更がある場合は停止し、既存作業を上書きしません。
シンボリックリンクと入れ子のGitメタデータは受け付けません。
コミット失敗時は変更を残すので、原因を確認して手動で保存してください。

DBディレクトリや秘密情報は入力しないでください。このコマンドは公開可否の
自動判定やHTMLの無害化は行いません。エクスポーターが安全なHTMLを生成し、
第三者データのライセンス・出典を保持する必要があります。

今後の公開URLは `https://open-ontologies.moukaeritai.work/ontologies/` を想定。
そのため、生成するリンク・アセットはこのサブパスに対応させます。
GitHub公開用のremoteは未設定です。取得間隔の途中の全更新を記録する操作ログではなく、
完成したエクスポートごとのスナップショット履歴です。
