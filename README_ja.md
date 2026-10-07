# Kongweave

[English](README.md) | [日本語](README_ja.md)

**decKとkongctlのための環境別オーバーレイ。**

Kongweaveは、Kongのネイティブ設定に環境ごとのパッチを指定した順序で適用する、Go製のオフラインCLIです。
共通のbaseを1つ管理し、環境によって異なるフィールドだけを変更できます。
変更対象は、配列の位置やUUIDではなく、安定したリソース識別子で指定します。

ネイティブYAMLと参照ファイルをまとめた、移動可能な出力ディレクトリを生成します。
KongやKonnectへの接続、テンプレートの実行、環境変数の値の読み取り、外部CLIの呼び出しは行いません。
Kongweaveは独立した初期段階のMVPであり、Kongの公式ツールではありません。

詳しい利用手順、設定リファレンス、CLIの使い方、トラブルシューティングは[利用ガイド](docs/guide_ja.md)を参照してください。

## なぜKongweaveが必要なのか

ホストやレート制限などの環境差分は、[decKの`file patch`](https://developer.konghq.com/deck/file/manipulation/patch/)でも、セレクターと複数のパッチファイルを使って適用できます。
決まったパッチコマンドで運用が完結するなら、追加のツールは必要ないかもしれません。

Kongweaveが役立つのは、環境構成の定義、変更対象の検証、生成結果の説明を、リポジトリ内の共通のルールとしてチームで揃えたい場面です。

- **環境の組み立て方を宣言する。** overlayにbaseまたは親overlayとパッチの適用順序を記載し、設定ファイルから継承関係と優先順位を確認できます。
- **変更対象に求める条件を明確にする。** `name`や`ref`とscopeでリソースを指定し、更新・削除では一致が必ず1件、リソースの追加では既存の一致が0件であることを要求します。
- **決まったマージ規則で部分変更する。** `config.policy`を保持したまま`config.minute`を変更でき、一般的な配列は全体を置換し、フィールドやリソースの削除は`remove`で明示できます。
- **生成された設定の変更元を辿る。** `kongweave explain`で、リソースやフィールドに影響したbaseとパッチのファイルを、設定値を表示せず適用順に確認できます。

パッチコマンドにスクリプトやCIの検証を組み合わせて、同様の運用を構築することもできます。
Kongweaveはこれらのルールと合成履歴を、decKとkongctlのネイティブYAMLを扱う1つのオフラインCLIにまとめ、プロジェクトごとに組み立て手順や検証処理を実装・保守する負担を減らします。
ソースファイルの時系列の変更はGitで管理し、現在の入力がどう合成されたかは`explain`で確認できます。
同じ入力から再現可能なバンドルを生成し、decKやkongctlによるネイティブの検証とデプロイに渡せます。

## インストール

[GitHub Releases](https://github.com/shukawam/kongweave/releases)からビルド済みアーカイブを取得します。
Go、decK、kongctlのインストールは不要です。
空の作業ディレクトリで実行してください。
ダウンロードは最初のリリース公開後に利用できます。

### macOS

次の例はApple Silicon向けです。
Intel Macでは`darwin_arm64`を`darwin_amd64`に変更してください。

```sh
(
  set -eu
  repository="shukawam/kongweave"
  archive="kongweave_darwin_arm64.tar.gz"
  url="https://github.com/$repository/releases/latest/download"
  curl -fL "$url/$archive" -o "$archive"
  curl -fL "$url/checksums.txt" -o checksums.txt
  grep -F "  $archive" checksums.txt | shasum -a 256 -c -
  tar -xzf "$archive"
  sudo install -d /usr/local/bin
  sudo install -m 755 kongweave /usr/local/bin/kongweave
  kongweave version
)
```

### Linux

次の例はx86_64向けです。
ARM64では`linux_amd64`を`linux_arm64`に変更してください。

```sh
(
  set -eu
  repository="shukawam/kongweave"
  archive="kongweave_linux_amd64.tar.gz"
  url="https://github.com/$repository/releases/latest/download"
  curl -fL "$url/$archive" -o "$archive"
  curl -fL "$url/checksums.txt" -o checksums.txt
  grep -F "  $archive" checksums.txt | sha256sum -c -
  tar -xzf "$archive"
  sudo install -d /usr/local/bin
  sudo install -m 755 kongweave /usr/local/bin/kongweave
  kongweave version
)
```

アーカイブのSHA-256を検証してから、展開とインストールを行います。
アーカイブには単体バイナリのほか、ドキュメントとサンプルも含まれます。
バージョンの固定、更新、sudoなしでのインストールは[インストールガイド](docs/guide_ja.md#インストールとサンプルのビルド)を参照してください。

## クイックスタート

アーカイブの展開先から、同梱のサンプルを使って実行します。

```sh
kongweave build examples/deck/overlays/dev --output dist/deck-dev
kongweave build examples/deck/overlays/prod --output dist/deck-prod
kongweave build examples/kongctl/overlays/dev --output dist/kongctl-dev
kongweave build examples/kongctl/overlays/prod --output dist/kongctl-prod

kongweave explain examples/deck/overlays/prod \
  --kind plugins --name rate-limiting --scope route=catalog-public \
  --field /config/minute
kongweave explain examples/kongctl/overlays/prod \
  --kind ai_gateway_models --ref example-chat --scope ai_gateway=example-ai \
  --field /config/route/paths --json
```

ビルドには毎回、**まだ存在しない出力ディレクトリ**を指定してください。
再度ビルドする場合は、別のパスを選びます。
既存の出力先は、空のディレクトリやシンボリックリンクであっても使用できません。
デフォルトの出力先は`<overlay-directory>/build`で、`--output`に明示した相対パスは実行時のカレントディレクトリを基準に解決します。
入力ファイル内の参照は、実行時のカレントディレクトリに関係なく、その参照を記述したファイルを基準に解決します。

生成されるバンドルの構成は次のとおりです。

```text
dist/kongctl-dev/
  config.yaml       # 未評価のタグを保持したネイティブYAML
  manifest.json     # 適用順の合成履歴。設定値は含まない
  assets/           # 評価せずにコピーした参照ファイル
```

移動するときは、バンドル全体をまとめて移動してください。
`config.yaml`がネイティブツールへの入力ファイルで、`manifest.json`はKongweaveのメタデータです。
`manifest.json`をdecKやkongctlへの入力に含めないでください。

## Baseとoverlay

```text
base/kong.yaml
overlays/common/overlay.yaml
overlays/dev/overlay.yaml
overlays/dev/patches/environment.yaml
```

baseには通常のdecK YAMLを使用します。

```yaml
_format_version: "3.0"
services:
  - name: catalog
    host: catalog.example.com
    plugins:
      - name: rate-limiting
        config:
          minute: 60
          policy: local
```

`overlays/dev/overlay.yaml`には、入力形式とパッチファイルの適用順序を記述します。

```yaml
apiVersion: kongweave/v1alpha1
format: deck
base:
  file: ../../base/kong.yaml
patches:
  - patches/environment.yaml
  - patches/plugins.yaml
```

別のoverlayを参照する場合は、`base: {overlay: ../common/overlay.yaml}`を指定します。
各overlayが参照できるbaseは、ちょうど1つです。
すべてのoverlayで入力形式を宣言する必要があり、形式の混在、リモートソース、循環参照はエラーになります。
シンボリックリンクを介した循環参照も検出します。
親overlayのパッチを先に適用し、続いて子overlayに列挙されたファイルをリスト順に、その各ファイル内の操作もリスト順に適用します。
同じパッチファイルを意図的に複数回指定することもできます。

`patches/environment.yaml`の例です。

```yaml
patches:
  - op: merge
    target: {kind: services, name: catalog}
    value: {host: catalog.dev.example.com}
  - op: merge
    target:
      kind: plugins
      name: rate-limiting
      scope: {service: catalog}
    value:
      config:
        minute: 600
```

この変更後も、既存の`config.policy: local`は保持されます。
パッチにはYAMLの値をそのまま記述でき、kongctlのタグ付き値を新しく追加することもできます。

## パッチの仕様

| 操作 | 前提条件 | 動作 |
| --- | --- | --- |
| `merge` | 既存の通常のマッピングにちょうど1件一致する | 通常のマップを再帰的にマージし、未指定のフィールドを保持する |
| `replace` | 既存の対象にちょうど1件一致する | リソース全体または選択したフィールドを置き換える |
| `add` | 対象が存在せず、親が存在する | リソースを末尾に追加するか、マッピングのフィールドを追加する |
| `remove` | 既存の対象にちょうど1件一致する | リソースまたはマッピングのフィールドを削除する。`value`は指定できない |

リソース全体を操作する場合、`value`にはリソースのマッピングを指定します。
フィールドを操作する場合、`field`には選択したリソースを基準とするJSON Pointerを指定します。
辿れるのはマッピングのキーだけで、`/config/minute`や、キーをエスケープした`/labels/a~1b`などを使用できます。
配列インデックス、ワイルドカード、JSONPath式、親の暗黙的な作成には対応していません。
フィールドへの`merge`では、既存の値と新しい値の両方が通常のマップである必要があります。
フィールドへの`replace`と`add`では、`null`を含む任意のYAML値を指定できます。

```yaml
patches:
  - op: remove
    target: {kind: plugins, name: rate-limiting, scope: {service: catalog}}
    field: /config/policy
  - op: add
    target: {kind: plugins, name: cors, scope: {service: catalog}}
    value: {name: cors, config: {origins: [https://app.example.com]}}
  - op: replace
    target: {kind: services, name: catalog}
    field: /host
    value: catalog.prod.example.com
```

* スカラーは指定した値で上書きし、未指定のキーは既存の値を保持します。
* 明示的な`null`は値であり、暗黙の削除指示にはなりません。`[]`は空配列で、`null`やフィールドの省略とは区別します。ネイティブスキーマが特定のフィールドで`null`を禁止している場合、Kongweaveを使ってもその値が有効になるわけではありません。
* 通常の配列は全体を置き換え、並べ替えません。要素に`name`があるだけではリソースとみなしません。AIモデルの`targets`、ポリシー参照のリスト、Plugin設定内の配列、Routeのパスは通常の配列として扱います。
* 既知のリソースコレクションは、まとめてマージしたり、`field`を通じて選択したりできません。個々のリソースを指定してください。リソース全体の`replace`では、その子リソースも明示的に置き換えられます。`remove`はネストされた子リソースも削除します。最後の要素を削除したリソース配列は空配列として残ります。
* 識別子とscopeは変更できません。リソースの名前や適用先を変えるには、`remove`の後に`add`を使用します。ネイティブの参照関係は自動では書き換えません。
* `value`内のscopeの扱いは操作によって異なります。単一scopeの`add`は親の下にリソースをネストし、scopeフィールドを書き込みません。複数の適用先を組み合わせたPluginの`add`はルートに追加し、`value`で省略された適用先のフィールドを固定のキー順で補完します。`replace`はリソース全体を指定するため、フラットなリソースでは`service`や`api`などの適用先のフィールドも再記述する必要があり、省略すると識別子の変更として失敗します。ネストされたリソースのscopeは親から決まるため、ネストされたリソースへの`replace`ではscopeフィールドを記述する必要はありません。
* `field`はRFC 6901のJSON Pointerです。`/`や`/a/`は空文字のキーを指定しますが、通常そのキーは存在しないため、既存の対象を要求する操作では一致件数のエラーになります。
* `!secret`などのタグ付きマッピングは、分割できない式として扱います。新しい式を指定すると式全体を置き換え、その内部フィールドをJSON Pointerで変更することはできません。
* 操作対象は1件です。一致する対象がなければエラーとなり、`merge`が暗黙に新規作成へ切り替わることはありません。複数件に一致する場合も失敗します。一致件数のエラーには、パッチファイルと行番号、対象指定、期待件数、実際の件数を含めます。識別子の重複はパッチ適用前と各操作の直後に検証し、後続のパッチで重複を解消する予定でもエラーになります。

## 対応する入力形式とリソース識別子

入力ファイルには、マッピング形式のYAMLドキュメントをちょうど1つ記述します。
Kongweaveはネイティブのリソーススキーマを**完全には検証しません**。
公式ツールによる検証も行ってください。

| 形式 | `kind` | 識別子と対応する配置 |
| --- | --- | --- |
| decK 3.0 | `services` | `name`、ルートに配置 |
| decK 3.0 | `routes` | `name`、ルートまたは`services[].routes`に配置。必要に応じて`scope.service`を指定 |
| decK 3.0 | `consumers` | セレクターの`name`でネイティブの`username`に一致させる。ルートに配置 |
| decK 3.0 | `plugins` | `name`、任意の`instance_name`、service・route・consumerの正確なscope。ルートまたは各適用先リソースの下にネスト |
| kongctl | `portals`、`apis`、`control_planes`、`application_auth_strategies`、`ai_gateways` | 明示的な`ref`、ルートに配置 |
| kongctl | `portal_pages` | `ref`、`portal`を指定してルートに配置するか、`portals[].pages`に配置。`scope.portal`を使用 |
| kongctl | `api_versions`、`api_documents`、`api_publications` | `ref`、`api`を指定してルートに配置するか、`apis[].versions/documents/publications`に配置。`scope.api`を使用 |
| kongctl | `ai_gateway_models`、`ai_gateway_model_providers`、`ai_gateway_policies` | `ref`、`ai_gateway`を指定してルートに配置するか、`ai_gateways[].models/model_providers/policies`に配置。`scope.ai_gateway`を使用 |

サンプルは[kongctlの宣言的設定リソースリファレンス](https://github.com/Kong/kongctl/blob/main/docs/declarative-resource-reference.md)に沿っています。
AIモデルの接続先、モデルターゲット、ルーティングの差分は、`targets[].config.upstream_url`、`targets[].name`、`config.route`を使って表現しています。

`scope`を省略すると、すべてのscopeを検索し、ちょうど1件に一致することを要求します。
`scope`を指定した場合は、**すべての関連付け**が完全に一致する必要があります。
`scope: {}`はグローバル、つまり適用先のないリソースを明示的に選択します。
`explain`では`--global`を使用します。
Routeの下にあるPluginのscopeはそのRouteであり、Routeが属するServiceはPluginの追加の適用先にはなりません。
複数の適用先は、`scope: {service: catalog, consumer: client}`のように指定します。
Pluginの追加では、必ずscopeを明示してください。
単一scopeのPluginは親の下にネストし、複数の適用先を組み合わせたPluginはルートに追加します。

対応するdecKのService、Route、Consumerには、安定した名前またはユーザー名が必要です。
フラットな参照関係では、文字列または`{name: ...}`を使用でき、Consumerでは`{username: ...}`を使用します。
UUIDだけで指定する参照関係と、Consumer GroupへのPluginの適用には対応していません。
Service名とRoute名は、それぞれのリソース種別内で全体を通して一意である必要があります。
Pluginの識別子には、scopeと任意の`instance_name`も含まれます。
空でないPluginの`instance_name`も、全体を通して一意である必要があります。
kongctlの`ref`は、対応するリソース種別をまたいで全体で一意である必要があります。
フラットに配置したkongctlの子リソースでは、親の指定にリテラルのrefまたはスカラーの`!ref`を使用でき、親は同じbase内で宣言されている必要があります。

## ネイティブYAMLと制約

KongweaveはYAMLノードを使い、スカラーの型と、未評価の`!ref`、`!file`、`!env`、`!secret`、`!lookup`、`!external`式を保持します。
タグの構文や対応するネスト構造は検証しますが、すべてのネイティブフィールドについて、そのタグを使用できるかまで検証するわけではありません。
データフィールド内の参照検索は未評価のまま保持しますが、lookupで決まる*親scope*は合成に使用できません。
詳しくは[ネイティブタグの仕様](https://github.com/Kong/kongctl/blob/main/docs/declarative.md#yaml-tags)を参照してください。

`!file`のスカラーパス（`#extract`を含む）と`{path, extract}`形式は、コピーしたアセットへのパスに書き換えます。
`_deck.files`はControl Plane上のものだけを扱います。
パッチで導入したパスは、その**パッチファイル**を基準に解決し、変更していないパスは元のbaseファイルを基準に解決します。
アセットは、参照を記述したファイルのディレクトリ内にあるローカルの通常ファイルに限り、シンボリックリンクの解決後もその範囲に収まる必要があります。
glob、URL、絶対パス、動的なパス、標準入力、許可範囲外へのパスは拒否します。
必要に応じて、共有アセットを参照元のディレクトリ内に移動してください。
`_deck`のアセットは、独自タグのない単一ドキュメントのdecK 3.0 YAMLである必要があります。
パスに見えるだけの任意の文字列は書き換えません。
空でない`_deck.flags`は、安全に書き換えられない追加のファイルパスを含む可能性があるため拒否します。

未評価の`!secret {source: !file ...}`の参照元を含め、ファイルの内容はそのままのバイト列としてコピーし、値の埋め込みや抽出は行いません。
バンドルには**参照元のファイル自体が含まれる**ため、公開可能なサンプルを使うか、機密情報を含む成果物として扱ってください。
Unixでは、ファイルをモード`0600`、ディレクトリを`0700`で作成します。
合成後に残る参照先だけを読み取ります。
入力ファイルとアセットはそれぞれ10 MiBまで、YAMLのネストは100階層まで、overlayの継承は64階層までです。

継承チェーンごとにネイティブのbaseを1つに限定することで、`_defaults`のファイル境界を維持し、その内容を変更せずに保持します。
追加したリソースには、そのbaseのファイルコンテキストが適用されます。
`_templates`と`_extends`は、展開や結合をせずに拒否します。
これらで継承される識別子やリソースを扱うと、対象の照合や変更元の追跡が曖昧になるためです。
YAMLのアンカー、エイリアス、マージキー、文字列以外のマッピングキー、複数ドキュメント、対応表にないルートのリソース種別、認識済みの未対応の子構造も拒否します。
コメントや元の書式は保持しません。

ネットワークI/O、環境変数や秘密情報の解決、形式間の変換、リモートbaseのダウンロード、デプロイ、任意コードの評価、状態管理、ドリフト検出は行いません。
ネイティブのplan・apply・syncと、それらに伴う検証はdecKとkongctlが担います。

## Explainと再現性

`explain`は合成処理を再実行し、変更元のファイル、行番号、操作、リソース識別子、書き込まれたフィールドのパスを適用順に表示します。
後続の操作で上書きされた変更、明示的な削除、リソース全体の操作によって削除・置換された子リソースも履歴に含まれます。
子リソースは直接選択してください。
親リソースの履歴に、子への個別の変更を集約することはありません。
フィールドを指定した場合は、その祖先への書き込み（配列全体の置換など）も含めますが、兄弟フィールドだけを変更したマージは含めません。
値の表示、タグの評価、デフォルト値の展開、ネイティブのテンプレートやプランナーの動作の予測は行いません。
リソース識別子、フィールド名、変更元のパスは表示されるため、これらのメタデータに認証情報を含めないでください。

ソースツリーとその内容が同じなら、ツリーや出力先を移動しても、同一の設定、アセット、manifestを生成します。
アセットのファイル名には内容のハッシュを使用し、履歴の変更元パスは入口となるoverlayからの相対パスで記録します。
マップの挿入順と配列の要素順を保持します。
ビルドではステージングディレクトリ内で出力を完成させ、1回のrenameで最終的な出力先に配置します。
既存のバンドルは変更せず、ビルドが失敗してもその内容は保持されます。
これは成果物を不可分に配置する仕組みであり、電源断に対する永続化の保証ではありません。
プロセスが異常終了すると、ステージングディレクトリや出力先と同じ親ディレクトリ内の`.kongweave-lock`ファイルが残る場合があります。
これらを削除する前に、ビルドが実行中でないことを確認してください。

## 公式ツールへの受け渡し

decKのオフライン検証は、環境ごとに個別に実行します。

```sh
deck file validate --analytics=false dist/deck-dev/config.yaml
deck file validate --analytics=false dist/deck-prod/config.yaml
```

kongctlのオフライン検証には、同梱の**サンプル用ルール**を使用します。
これは完全なスキーマ検証やリモート検証ではありません。

```sh
kongctl --no-telemetry lint -f dist/kongctl-dev/config.yaml -r examples/kongctl/ruleset.yaml
kongctl --no-telemetry lint -f dist/kongctl-prod/config.yaml -r examples/kongctl/ruleset.yaml
```

後で別途承認を得てデプロイする場合は、ネイティブのワークフローに`config.yaml`を明示的に渡してください。
たとえば、`deck gateway diff dist/deck-prod/config.yaml`や`kongctl plan -f dist/kongctl-prod/config.yaml --output-file plan.json`を使用します。
これらのコマンドには適切な接続先、認証情報、ネットワーク接続が必要であり、**オフライン検証ではありません**。
Kongweaveがこれらを実行することもありません。
アセットまで設定ファイルとして読み込まれるため、バンドル全体を再帰的に読み込まないでください。
サンプルには架空のホスト名を使用しているため、そのままではトラフィックを処理できません。
ビルドにトークンは不要で、AIサンプルの未評価のトークンは、後からネイティブツールを実行する際に使用するものです。

## 開発

以下はソースのチェックアウトとGoが必要な開発者向けの検証であり、ビルド済みバイナリのインストールには不要です。

```sh
go test ./...
go test -race ./...
go vet ./...
```

CIではCLIをビルドし、合成仕様のテストと両形式のサンプルを検証します。
decKとkongctlがインストールされた環境では、必要に応じて`scripts/verify-offline.sh`でローカルの一連の処理を確認できます。
再現可能な確認手順とその検証範囲は、[検証と公式ツールへの受け渡し](docs/guide_ja.md#検証と公式ツールへの受け渡し)を参照してください。

ライセンスは[Apache-2.0](LICENSE)です。
