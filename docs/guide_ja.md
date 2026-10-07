# Kongweave利用ガイド

[ドキュメント一覧](README.md) | [English](guide.md)

Kongweaveは、1つの共通baseと順序を明示した環境別overlayの継承チェーンから、decKまたはkongctlのネイティブYAMLを生成します。
合成処理はオフラインで行い、KongやKonnectへの接続、秘密情報の解決、ネイティブテンプレートの展開、成果物のデプロイは行いません。
2つの入力形式は、処理全体を通して別々に扱います。

## 目次

1. [インストールとサンプルのビルド](#インストールとサンプルのビルド)
2. [環境別overlayの作成](#環境別overlayの作成)
3. [Overlayとパッチのリファレンス](#overlayとパッチのリファレンス)
4. [リソースの対象指定](#リソースの対象指定)
5. [ネイティブYAMLと参照ファイル](#ネイティブyamlと参照ファイル)
6. [CLIリファレンス](#cliリファレンス)
7. [変更履歴の確認](#変更履歴の確認)
8. [バンドルと再現性](#バンドルと再現性)
9. [検証と公式ツールへの受け渡し](#検証と公式ツールへの受け渡し)
10. [トラブルシューティング](#トラブルシューティング)

## インストールとサンプルのビルド

### リリースのダウンロード

macOS・Linuxのarm64・amd64向けに、ビルド済みアーカイブを配布します。
Kongweaveのインストールと実行に、Go、decK、kongctlは不要です。
最初のリリース公開後、空の作業ディレクトリで実行してください。
次の例はOSとCPUを判定し、チェックサムを検証してから、sudoを使わずユーザーディレクトリにインストールします。

```sh
(
  set -eu
  repository="shukawam/kongweave"
  url="https://github.com/$repository/releases/latest/download"
  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) echo "Unsupported operating system" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) arch=arm64 ;;
    x86_64|amd64) arch=amd64 ;;
    *) echo "Unsupported architecture" >&2; exit 1 ;;
  esac
  archive="kongweave_${os}_${arch}.tar.gz"
  curl -fL "$url/$archive" -o "$archive"
  curl -fL "$url/checksums.txt" -o checksums.txt
  if [ "$os" = darwin ]; then
    grep -F "  $archive" checksums.txt | shasum -a 256 -c -
  else
    grep -F "  $archive" checksums.txt | sha256sum -c -
  fi
  tar -xzf "$archive"
  mkdir -p "$HOME/.local/bin"
  install -m 755 kongweave "$HOME/.local/bin/kongweave"
  "$HOME/.local/bin/kongweave" version
)
```

現在のシェルで、インストール先をPATHに追加します。

```sh
export PATH="$HOME/.local/bin:$PATH"
kongweave version
```

次回以降も使うには、`~/.zshrc`や`~/.bashrc`にこのPATH設定を追加してください。
システム全体にインストールする場合は、`/usr/local/bin`に配置する[READMEの手順](../README_ja.md#インストール)を使用します。
アーカイブには`docs/`、`examples/`、README、ライセンスを含むので、サンプルを試す場合は展開先を保持してください。

### バージョンとプラットフォームの選択

| プラットフォーム | アーカイブ |
| --- | --- |
| macOS Apple Silicon | `kongweave_darwin_arm64.tar.gz` |
| macOS Intel | `kongweave_darwin_amd64.tar.gz` |
| Linux x86_64 | `kongweave_linux_amd64.tar.gz` |
| Linux ARM64 | `kongweave_linux_arm64.tar.gz` |

`releases/latest/download`は最新の安定版を選択します。
再現可能なインストールやプレリリースの利用には、公開済みのタグを指定してください。

```sh
url="https://github.com/$repository/releases/download/v0.1.0"
```

ダウンロード手順内のURLをこの形式に変え、`v0.1.0`を目的の公開済みタグに置き換え、アーカイブと`checksums.txt`を同じリリースから取得してください。
チェックサムはダウンロードの破損や取り違えを検出するもので、発行者の署名ではありません。
更新やダウングレードは目的のリリースを再インストールし、`kongweave version`で確認します。
アンインストールするときは、インストール先の実行ファイルだけを削除してください。
このリリースワークフローでは、HomebrewパッケージとWindowsバイナリは配布しません。

### サンプルのビルド

アーカイブの展開先から、次のコマンドを実行します。

```sh
kongweave version
kongweave build examples/deck/overlays/dev --output dist/deck-dev
kongweave build examples/deck/overlays/prod --output dist/deck-prod
kongweave build examples/kongctl/overlays/dev --output dist/kongctl-dev
kongweave build examples/kongctl/overlays/prod --output dist/kongctl-prod
```

再実行する場合も、出力先には毎回まだ存在しないディレクトリを指定してください。
生成したネイティブ設定は`config.yaml`、適用順の合成履歴は`manifest.json`で確認できます。
`assets/`への参照を維持するため、移動やアーカイブは出力ディレクトリ全体を対象に行ってください。
サンプルのホスト名や認証情報は架空の値または未評価の参照であり、ビルドにアカウントやトークンは不要です。

decKのサンプルでは、Serviceのホストとタイムアウト、RouteのHTTPメソッド、異なるscopeに適用されるPluginを変更します。
kongctlのサンプルでは、APIドキュメント、AI Gatewayのメタデータ、モデルの接続先、ルーティング、サニタイザーポリシーの設定を変更します。
モデルの`targets`とその中の`name`は通常の設定データなので、配列を置き換える際は、保持したいターゲットもすべて記述してください。

## 環境別overlayの作成

ここでは、既存のdecKサンプルにstaging環境を追加します。
共通baseを変更せず、`examples/deck/overlays/staging/`の下に次の2つのファイルを作成してください。

```text
examples/deck/
  base/kong.yaml
  overlays/
    common/
      overlay.yaml
      timeouts.yaml
    staging/
      overlay.yaml
      patches/environment.yaml
```

`examples/deck/overlays/staging/overlay.yaml`を作成します。

```yaml
apiVersion: kongweave/v1alpha1
format: deck
base:
  overlay: ../common/overlay.yaml
patches:
  - patches/environment.yaml
```

`examples/deck/overlays/staging/patches/environment.yaml`を作成します。

```yaml
patches:
  - op: merge
    target: {kind: services, name: catalog}
    value:
      host: catalog.staging.example.com
      read_timeout: 8000
  - op: merge
    target:
      kind: plugins
      name: rate-limiting
      scope: {route: catalog-public}
    value:
      config:
        minute: 120
  - op: replace
    target: {kind: routes, name: catalog-public}
    field: /methods
    value: [GET, HEAD]
  - op: add
    target: {kind: plugins, name: cors, scope: {service: catalog}}
    value:
      name: cors
      config:
        origins: [https://app.staging.example.com]
  - op: remove
    target: {kind: plugins, name: response-transformer, scope: {}}
```

staging環境の設定をビルドし、履歴を確認します。

```sh
kongweave build examples/deck/overlays/staging --output dist/deck-staging
kongweave explain examples/deck/overlays/staging --kind plugins --name rate-limiting --scope route=catalog-public --field /config/minute
```

Serviceは共通overlayの`write_timeout: 10000`を継承し、staging用のホストと読み取りタイムアウトを使用します。
Routeに適用されたレート制限は`120`となり、既存の`policy: local`と`limit_by: ip`は保持されます。
Service側にある別のrate-limiting Pluginは変更されません。
Routeは`GET`と`HEAD`を受け付け、ServiceにはCORS Pluginが追加され、グローバルなresponse-transformer Pluginは削除されます。

kongctlの場合は`format: kongctl`を指定し、ネイティブのkongctl設定をbaseとして参照し、対象を`ref`で指定します。
既存の[本番用AI Gatewayパッチ](../examples/kongctl/overlays/prod/patches/environment.yaml)では、`config.route.paths`、`targets`配列全体、別途選択したポリシーの`config.anonymize`を変更しています。
マップの部分マージなので、`config.route.model`や`config.logging`などのフィールドは保持されます。

## Overlayとパッチのリファレンス

### Overlayファイル

CLIは、`build`または`explain`に渡したディレクトリ内の`overlay.yaml`を読み込みます。
overlayに指定できるキーは次のとおりです。

| キー | 必須か | 意味 |
| --- | --- | --- |
| `apiVersion` | 必須 | `kongweave/v1alpha1`を指定 |
| `format` | 必須 | `deck`または`kongctl`。継承チェーン全体で一致させる |
| `base.file` | baseの参照を1つ指定 | ネイティブYAMLファイルへの相対パス |
| `base.overlay` | `base.file`の代わりに指定 | 別のoverlay YAMLへの相対パス。ファイル名まで含める |
| `patches` | 任意 | 適用順に並べたパッチファイルの相対パス。省略または`[]`なら、そのoverlay自身のパッチは適用しない |

`base.file`と`base.overlay`は、どちらか一方だけを指定してください。
すべての入力参照は、シェルのカレントディレクトリではなく、その参照を記述したファイルを基準に解決します。
たとえば、`overlays/dev/overlay.yaml`から`base/kong.yaml`を参照する場合は、`../../base/kong.yaml`を指定します。
ローカルのbaseやパッチへの参照では`..`を使用できますが、コピーするアセットには後述のより厳しいディレクトリ制約があります。

最初にネイティブのbaseを読み込み、次に祖先のoverlayを最も古いものから順に適用し、最後に選択したoverlayを適用します。
各overlayのパッチファイルはリスト順に適用し、そのファイル内の操作もリスト順に適用します。
同じパッチファイルを2回列挙すると2回適用するため、`add`や`remove`は2回目で失敗することがあります。
リモート参照、循環参照、形式の混在、64階層を超える継承は拒否します。

### 操作

パッチファイルには、空でない`patches`配列を記述します。
各要素には`op`、`target`、任意の`field`と、`remove`以外の操作では`value`を指定します。
`field`を省略するか`""`を指定すると、リソース全体を対象にします。

| 操作 | リソース全体 | リソース内のフィールド |
| --- | --- | --- |
| `merge` | 既存リソースにちょうど1件一致し、通常のマッピングを再帰的にマージする | リソースとフィールドが一意に存在し、既存の値と新しい値がどちらも通常のマップであることが必要 |
| `replace` | 既存リソースにちょうど1件一致し、子を含むマッピング全体を置き換える | フィールドが存在することが必要。任意のYAML値で置き換える |
| `add` | 一致するリソースが存在せず、親が存在することが必要。リソースのマッピングを追加する | リソースと格納先のマップが存在し、フィールドが存在しないことが必要 |
| `remove` | 既存リソースにちょうど1件一致し、ネストされた子とともに削除する | フィールドが存在することが必要。マッピングのエントリーを削除する |

`merge`、`replace`、`add`では`value`が必須です。
明示的な`value: null`は値の指定であり、`value`の省略とは異なります。
`remove`では、`value: null`を含め、`value`を指定できません。
スカラー、配列、nullを`value`全体として使用できるのは、フィールドに対する`add`と`replace`だけです。
リソース全体を操作する場合は、通常のマッピングを指定してください。
対象や格納先のマップを暗黙に作成する操作はありません。

### マージの動作

| 新しいデータ | 結果 |
| --- | --- |
| 通常のマップに対する通常のマップ | 再帰的にマージし、未指定のキーを保持する |
| スカラー | 指定した値で置き換え、そのYAML型を保持する |
| `null` | nullを格納し、キーは削除しない |
| 通常の配列 | 要素の照合や並べ替えをせず、配列全体を置き換える |
| `[]` | 空配列を格納する |
| 通常のマップに対する空の通常のマップ | 既存のエントリーを保持する |
| タグ付きの式 | 式全体を置き換える |
| キーの省略 | 既存の値を保持する |

`services[].plugins`などの既知のリソース配列は、個々のリソースを選択して編集してください。
親リソース経由でマージしたり、`field`で配列を辿ったりすることはできません。
リソース全体の`replace`は、そのリソースの部分木全体を意図的に置き換えるため、ネストされた子も変更できます。
リソース配列の最後の要素を削除すると、配列は`[]`として残ります。

### フィールドのパス

`field`にはRFC 6901のJSON Pointer構文を使用し、マッピングのキーだけを指定できます。
ネストされたキーには`/config/minute`のようなパスを使い、キー内の`/`は`~1`、`~`は`~0`でエスケープします。
たとえば、`/labels/team~1owner`は`labels`内の`team/owner`というキーを指定します。
`/`はリソース直下の空文字のキー、`/config/`は`config`内の空文字のキーを指定します。
これらのパスにも、各操作が要求する存在条件が適用されます。
配列インデックス、ワイルドカード、JSONPath、タグ付きの式の内部を辿る操作には対応していません。
通常の配列の要素を1つ変更したい場合は、必要な要素を意図した順序で並べ、配列全体を置き換えてください。

### 識別子と参照関係

リソース名、ref、Pluginのインスタンス名、scopeは、`merge`や`replace`では変更できません。
`instance_name`のなかったPluginにそのフィールドを追加することも、識別子の変更に該当します。
名前や適用先を変更する場合は、明示的に`remove`と`add`を使用し、他の参照も利用者側で更新してください。
baseと各操作後の結果は、有効かつ一意な識別子を持つ必要があり、不正な中間状態を後の操作で修復することはできません。
フラットに配置された子は親より先に削除してください。
親を先に削除すると、子の親参照を解決できずエラーになります。
ネストされた子は、親とともに自動的に削除されます。

単一の親scopeを持つ`add`は、親の下に新しいリソースをネストし、適用先のフィールドを自動追加しません。
複数の適用先を組み合わせたPluginの`add`はルートに追加し、省略された適用先のフィールドを固定順序で補完します。
フラットなリソースの`replace`では、`service`や`api`などの既存の適用先フィールドも、置き換え後のマッピングに含める必要があります。
省略するとscopeの変更になり、操作は失敗します。
ネストされたリソースの`replace`では、scopeが親から決まるため、これらのフィールドを省略できます。

## リソースの対象指定

対象は、リソースがネストされている場所にかかわらず、ネイティブの複数形のリソース種別で指定します。
次の表は対応するリソースコレクションの一覧であり、各リソースのネイティブフィールドを完全に検証するスキーマではありません。

| 形式 | 種別 | 識別子 | 対応する配置とscope |
| --- | --- | --- | --- |
| decK 3.0 | `services` | `name` | ルート |
| decK 3.0 | `routes` | `name` | ルートまたは`services[].routes`。任意の`service` scope |
| decK 3.0 | `consumers` | セレクターの`name`で`username`に一致させる | ルート |
| decK 3.0 | `plugins` | `name`、任意の`instance_name`、適用先 | ルートまたはService・Route・Consumerの下。`service`、`route`、`consumer` scope |
| kongctl | `portals`、`apis`、`control_planes`、`application_auth_strategies`、`ai_gateways` | `ref` | ルート |
| kongctl | `portal_pages` | `ref` | `portal`を指定したルート、または`portals[].pages`。`portal` scope |
| kongctl | `api_versions`、`api_documents`、`api_publications` | `ref` | `api`を指定したルート、または`apis[].versions/documents/publications`。`api` scope |
| kongctl | `ai_gateway_models`、`ai_gateway_model_providers`、`ai_gateway_policies` | `ref` | `ai_gateway`を指定したルート、または`ai_gateways[].models/model_providers/policies`。`ai_gateway` scope |

対応するdecKのService、Route、Consumerには、安定した名前またはユーザー名が必要です。
Routeが異なるServiceの下にネストされていても、識別子はそれぞれのリソース種別全体で一意である必要があります。
空でないPluginのインスタンス名は、全体で一意である必要があります。
kongctlのrefは明示的な文字列で、対応するすべてのリソース種別をまたいで一意である必要があります。
UUIDだけで指定する識別子や、後から解決される識別子は合成の対象指定に使用できません。

### Pluginのscope

`scope`を省略すると、すべてのscopeを検索しますが、一致する対象は1件である必要があります。
`scope: {}`は、適用先を持たないグローバルなリソースを明示的に選択します。
空でないscopeを指定する場合は、すべての適用先が完全に一致する必要があります。
たとえば、`{service: catalog}`は、そのServiceとConsumerの両方に適用されるPluginには一致しません。

```yaml
target: {kind: plugins, name: rate-limiting, scope: {route: catalog-public}}
```

`catalog-public`の下にネストされたPluginはRouteのscopeだけを持ち、Routeが属するServiceが自動的に追加の適用先になることはありません。
複数の適用先には、`{service: catalog, consumer: client}`のようなscopeを使用し、両方の親リソースを宣言してください。
同じPluginの複数のインスタンスをさらに区別する必要がある場合は、`instance_name`を指定します。
`instance_name`を省略するとすべてのインスタンスを検索するため、広い条件では複数件に一致する可能性があります。
Pluginの`add`では、グローバルなPluginの`{}`も含め、必ずscopeを明示してください。
Consumer GroupへのPluginの適用には対応していません。

フラットなdecKの参照関係では、安定した文字列、`{name: ...}`、Consumerの場合は`{username: ...}`を指定できます。
フラットなkongctlの子リソースの親参照には、リテラルのrefまたはスカラーの`!ref`を使用でき、操作を検証する時点で親がローカルに宣言されている必要があります。
Kongweaveはリモートの親scopeを解決しません。

## ネイティブYAMLと参照ファイル

各ファイルには、マッピング形式のYAMLドキュメントをちょうど1つ記述してください。
スカラーの型、null、配列の順序、対応するタグは保持しますが、コメントや書式は保持しません。
マッピングキーの重複、文字列以外のキー、YAMLマージキー、アンカー、エイリアスは拒否します。

### 未評価のkongctlタグ

| タグ | 対応する式の形式 | ビルド時の動作 |
| --- | --- | --- |
| `!ref` | スカラーの`ref`または`ref#field` | 参照を未評価のまま保持する |
| `!file` | スカラーパスと任意の`#extract`、または`{path, extract}` | ファイルをコピーしてパスを書き換え、抽出式は実行せず保持する |
| `!env` | スカラーの変数名と任意の`#extract`、または`{var, extract}` | 環境変数を読み取らず保持する |
| `!secret` | `source`または`parts`のどちらか一方を持つマッピング | 式を解決せず保持する |
| `!lookup`、`!external` | スカラーの`field:value`または空でないセレクターのマッピング | リモート検索を行わず保持する |

`!file`と`!env`のマッピング形式では、通常の文字列の`path`または`var`が必要で、任意の`extract`も文字列である必要があります。
これらの内容にはタグをネストできません。
`!secret.source`は`!env`または`!file`である必要があります。
`!secret.parts`は、通常の文字列、`!env`、`!file`からなる空でないリストで、少なくとも1つの未評価の参照元を含む必要があります。
lookupのマッピング値は文字列または直接の`!env`式である必要があり、`id`と他のセレクターは併用できません。
タグの構文検証は、すべてのネイティブフィールドでそのタグを使用できることを保証するものではありません。
最終的なフィールドとの適合性はネイティブツールで検証してください。
decK入力の独自タグには対応していません。

タグ付きマップは分割できない式として扱うため、秘密情報の式の`parts`を直接変更するのではなく、式全体を置き換えてください。
パッチで追加したタグも、baseのタグと同じく未評価のまま保持します。
[kongctlのbaseサンプル](../examples/kongctl/base/config.yaml)には、後から`"Bearer "`と`!env KONGWEAVE_DEMO_TOKEN`を組み合わせるAuthorization値がありますが、ビルド時にこの変数を読み取ることはありません。

### 移動可能なアセット

base内のファイル参照はbaseファイルを基準に解決し、パッチで追加した参照はそのパッチファイルを基準に解決します。
同じリソースの別のフィールドを変更しても、未変更の参照は元のファイルコンテキストを保持します。
各アセットは、その参照を記述したファイルのディレクトリ内か、その配下に配置してください。
シンボリックリンクの解決後も、この範囲に収まる必要があります。
絶対パス、リモートURL、標準入力、動的なパス、glob、そのディレクトリの外に出るパスは拒否します。

たとえば、`patches/environment.yaml`に`!file docs/catalog.md`を記述した場合は、`patches/docs/catalog.md`が必要です。
[kongctlのdevパッチ](../examples/kongctl/overlays/dev/patches/environment.yaml)と、その隣の`docs/`ディレクトリでこの配置を確認できます。
コピーしたファイルは`assets/<sha256><extension>`に保存し、生成する参照先もそのパスに変更します。
合成後に残る参照先だけをコピーします。
参照ファイルの内容はそのままのバイト列として扱い、別の設定ツリーとして再帰的に評価することはありません。

Control Planeの`_deck.files`の参照先もコピーし、パスを書き換えます。
これらのアセットは、独自タグを含まない単一ドキュメントのdecK 3.0ファイルである必要があります。
空でない`_deck.flags`は、確実に移動できない追加のパスを含む可能性があるため未対応です。
パスに見えるだけの他の文字列を、ファイル参照と推測することはありません。

未評価の秘密情報であっても、参照元が`!file`なら、そのファイルの内容はバンドルにコピーされます。
公開するサンプルには架空のデータを使い、実際の秘密情報を含むバンドルはその内容に応じて扱ってください。
Unixでは、ファイルのモードは`0600`、ディレクトリのモードは`0700`です。
入力ファイルとアセットはそれぞれ10 MiBまで、YAMLのネストは100階層までです。

### デフォルト値、テンプレート、未対応の構造

`_defaults`は単一のネイティブbase内で変更せず保持し、追加したリソースにも、ネイティブツールが処理する際にそのドキュメントのコンテキストが適用されます。
ルートのメタデータを変更するパッチ操作はないため、ルートのデフォルト値を変える場合は別のbaseを使用するか、リソースのメタデータを明示的に変更してください。
`_templates`と`_extends`は未対応で、エラーになります。
Kongweaveを使う前に入力を展開または再構成してください。
Kongweave自身はネイティブのテンプレート評価を実装していません。
一覧にないルートのリソース種別や、認識済みの未対応の子構造は拒否します。
これにはConsumerの認証情報、再帰的なPortalページの子、対応表にないAI Gatewayのリソース群が含まれます。
未対応の設定はそのまま通過すると考えず、別のネイティブワークフローで扱うよう分離してください。

## CLIリファレンス

コマンドのフラグより先に、overlayのディレクトリを指定してください。
`overlay.yaml`というファイル名ではなく、そのファイルを含むディレクトリを指定します。

| コマンドまたはオプション | 意味 |
| --- | --- |
| `kongweave build <dir>` | 新しい`<dir>/build`バンドルを生成する |
| `--output <dir>`、`-o <dir>` | 新しい出力先を指定する。相対パスは実行時のカレントディレクトリ基準 |
| `kongweave explain <dir>` | メモリ上で合成を再実行し、履歴を表示する。出力バンドルは作成しない |
| `--kind <kind>` | explainで対象にするリソース種別。必須 |
| `--name <name>` | decKの名前。Consumerではユーザー名。decKで必須 |
| `--ref <ref>` | kongctlのref。`--name`の代わりに指定し、kongctlで必須 |
| `--scope key=value` | 完全一致させるscope。Pluginの複数の適用先には繰り返し指定 |
| `--global` | 空のscopeを明示する。`--scope`とは併用不可 |
| `--instance-name <name>` | 任意のdecK Pluginのインスタンス指定 |
| `--field <pointer>` | フィールドと、その値に影響する書き込みに履歴を絞る |
| `--json` | 適用順の履歴をJSONで出力する |
| `--help`、`-h` | helpを表示する。`kongweave build --help`でも使用可能 |
| `kongweave version` | バイナリのバージョン文字列を表示する |

`build`は成果物をファイルへ、成功メッセージをstderrへ書き込み、stdoutには出力しません。
`explain`、help、versionの出力先はstdoutで、エラーはstderrに出力し、終了コード1を返します。
正常終了時のコードは0です。
`--name --help`や`--output -h`のように、文字列フラグの値がフラグに見える場合も、値として扱います。

## 変更履歴の確認

パッチの対象指定と同じ識別子と、正確なscopeを使用します。

```sh
kongweave explain examples/deck/overlays/prod --kind plugins --name rate-limiting --scope route=catalog-public --field /config/minute
kongweave explain examples/kongctl/overlays/prod --kind ai_gateway_models --ref example-chat --scope ai_gateway=example-ai --field /config/route/paths --json
```

テキスト出力では、各書き込みの順序、変更元パス、行番号、操作、対象、フィールドのパスを表示します。
順序番号は合成全体を通した番号なので、絞り込んだ履歴には番号の飛びが発生します。
baseの宣言とすべての明示的な書き込みを記録し、同じ値の再指定や、後で上書き・削除された変更も含めます。
リソース全体の置き換えや削除では、ネストされた子リソースへの影響も記録します。

フィールドを指定した場合、その祖先フィールドやリソース全体の置き換えも結果に含まれます。
兄弟フィールドだけを変更したマージは含めません。
子リソースは直接選択してください。
Serviceの履歴に、別途Pluginを対象にして行った変更を集約することはありません。
削除されたリソースの履歴も確認でき、対象指定は削除済みのものも含めて過去の1つのリソースに一致する必要があります。
過去の複数のPluginインスタンスに一致する場合は、正確なscopeとインスタンス名を指定してください。

履歴には設定値を含めず、ネイティブ側のデフォルト値、テンプレート展開、秘密情報の解決、リモート検索も対象にしません。
リソース識別子、パス、フィールド名は表示されるため、これらのメタデータに認証情報を入れないでください。
`explain`は現在のソースファイルを読み込み、過去のバンドルのmanifestを読み込んだり、パッケージングによるアセットの存在確認を行ったりすることはありません。
参照ファイルの検証には`build`を使い、特定のビルドの履歴を確認する場合は、保存したバンドルの`manifest.json`を参照してください。

## バンドルと再現性

| エントリー | 用途 |
| --- | --- |
| `config.yaml` | パスを書き換えたファイル参照を含む、ネイティブツールへの入力 |
| `assets/` | 参照ファイルの内容。ファイル参照がなければ作成されない |
| `manifest.json` | 形式、設定ファイルのパス、ファイル一覧、設定値を含まない適用順の履歴 |

ソースツリーと内容が同じなら、カレントディレクトリや、ソースツリーとバンドルの配置場所にかかわらず、同一のバンドル内容を生成します。
後からネイティブツールで適用したときに、同じ環境変数の値やリモートリソースが解決されることまで保証するものではありません。
配列の要素順とマップの挿入順を保持し、アセット名は内容のハッシュから決定します。
履歴の変更元パスは、入口となるoverlayからの相対パスです。

ビルドでは、ステージングディレクトリ内で成果物を完成させてから、最終的な出力先へrenameします。
そのパスに既存のファイル、ディレクトリ、シンボリックリンクがある場合は、上書きしません。
CIジョブごとのアーティファクトディレクトリなど、ビルドごとに異なる出力先を選び、バンドル全体をアーカイブしてください。
通常のビルド失敗では、最終出力先に不完全なディレクトリを残したり、既存のバンドルを変更したりしません。
プロセスの中断でステージングファイルや隣接する`.kongweave-lock`が残ることがありますが、削除する前にビルドが実行中でないことを確認してください。
不可分な配置は、電源断に対する永続化の保証ではありません。

## 検証と公式ツールへの受け渡し

Kongweaveは合成ルール、対象指定、識別子の重複、対応する構造、タグの形式、移動可能なファイル参照を検証します。
ネイティブのスキーマ、Plugin設定、認証、接続先、リモートの参照関係を完全に検証するものではありません。

decKをインストールしている場合は、環境ごとに個別に検証します。

```sh
deck file validate --analytics=false dist/deck-dev/config.yaml
deck file validate --analytics=false dist/deck-prod/config.yaml
```

decKは複数の入力ファイルを1つの状態として扱い、リソースの重複が発生する可能性があるため、devとprodをまとめて渡さないでください。
同梱のkongctlルールはサンプルの一部の性質を検証するもので、完全なネイティブ読み込みやリモートAPIとの互換性を検証するものではありません。

```sh
kongctl --no-telemetry lint -f dist/kongctl-dev/config.yaml -r examples/kongctl/ruleset.yaml
kongctl --no-telemetry lint -f dist/kongctl-prod/config.yaml -r examples/kongctl/ruleset.yaml
```

実際にデプロイする場合は、通常のネイティブワークフローにバンドルの`config.yaml`を明示的に渡し、適用前にネイティブのplanやdiffを確認してください。
たとえば、`deck gateway diff dist/deck-prod/config.yaml`と`kongctl plan -f dist/kongctl-prod/config.yaml --output-file plan.json`には、接続先の設定、認証情報、ネットワーク接続が必要です。
これらはオフライン検証ではなく、Kongweaveが実行することもありません。
コピーしたアセットが追加の設定ファイルと誤認される可能性があるため、バンドル全体を再帰的に読み込まないでください。
実環境で使用する前に、架空のサンプルを目的の環境に合わせて変更し、ネイティブツールを通じて未評価の値を渡してください。

## トラブルシューティング

| エラーまたは症状 | 確認すること |
| --- | --- |
| `expected 1, actual 0` | 種別、name/ref、正確なscope、先行操作による削除を確認する。フィールドのエラーでは、そのフィールドが存在するか確認する |
| `expected 1, actual 2`以上 | 正確なscopeやPluginのインスタンス名を追加する。explainでは削除済みの識別子も対象になる |
| `expected 0, actual 1` | `add`の対象リソースまたはフィールドが既に存在する。意図に合った明示的な更新操作を使う |
| 識別子の重複 | baseまたは重複を導入した操作を修正する。各操作後の中間状態も有効である必要がある |
| `identity and scope are immutable` | 識別子と適用先のフィールドをすべて保持するか、remove/addを使って関連する参照も更新する |
| `field parent must exist` | 格納先の通常のマップを先に作成またはマージする。配列やタグ付きの式の内部は辿らない |
| リソースコレクションのエラー | 親の配列をパッチで変更するのではなく、子のリソース種別を直接選択する |
| 未対応の種別、子構造、タグ | 対応表を確認し、未対応の入力を分離する。未知の構造は推測して扱われない |
| overlayの循環参照 | シンボリックリンクも含めて`base.overlay`の参照を辿る。チェーンは1つの`base.file`で終わる必要がある |
| ファイル参照のエラー | 参照を記述したディレクトリ、ファイルの存在、シンボリックリンク先を確認する。パッチで導入したパスはパッチ側を基準にする |
| `output already exists` | 既存のディレクトリが空でも、別の新しい出力先を選ぶ |
| 出力先のロックエラー | 残ったロックを削除する前に、実行中の書き込み処理と出力先の親ディレクトリの権限を確認する |
| `provide overlay directory before flags` | `build`または`explain`の直後にディレクトリを指定する。ハイフンで始まるディレクトリには`./-name`を使う |
| フィールドの合成履歴がない | 選択したリソースとフィールドを確認する。ネイティブのデフォルト値や後から行うタグの解決は履歴の対象外 |

診断メッセージでは、設定値やYAMLパーサーの生の詳細を意図的に省略します。
エラーメッセージに秘密情報や設定値が表示されることを期待せず、示されたファイルと行番号を使ってローカルでソースを確認してください。
Kongweaveが解決する環境設定の課題は、[なぜKongweaveが必要なのか](../README_ja.md#なぜkongweaveが必要なのか)を参照してください。
