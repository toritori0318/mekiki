# mekiki 設計仕様書

**読者:** mekiki を実装・拡張する人、および規則の厳密な定義を必要とするスキル作成者。
**他文書との関係:** [SKILL_PROTOCOL.ja.md](../SKILL_PROTOCOL.ja.md) が作者向けの規約で、
本書はそれを「どう検査するか」を定める。
**実測値について:** 数値と例の型は、mekiki が最初に適用されたコーパス（6プラグイン・約190スキル）
に由来する。設計の根拠として読み、自分の環境の制約とは読み替えること。

English: **[DESIGN.md](DESIGN.md)**

---

## 1. 背景と目的

スキル資産が数十〜数百に育つと、決定的処理の散文化・共有ファイルの物理コピー・発火条件の無規約・ガード宣言漏れ・検証不在が反復発生する（適用元コーパス165スキルの実監査で確認）。本書は、これらを機械的に防止する規約（SkillProtocol）と、その検証ツール `mekiki`・雛形生成 `mekiki new` の実装仕様を定義する。

## 2. スコープ

**含む:**
- スキル規約の厳密定義（frontmatter スキーマ、命名、Contract、ディレクトリ、データ形式、ガード、eval）
- `mekiki` の実装仕様（ルール全定義・CLI・出力形式・テストケース）
- `mekiki new` の実装仕様

**含まない:**
- 適用先の既存スキルの改修そのもの（lint の指摘に基づき各組織で実施）
- hook スクリプト本体の実装（各組織のガード実装に委ねる）
- eval runner の実装（本書はスキーマのみ定義。実行は `skill-creator` に委譲する — §4.8）

## 3. 前提・制約

- 実行環境: Go 1.24+（標準ライブラリのみ。外部依存を追加しない — 供給網の規律を扱うツール自身が供給網を持たないため）
- 検査対象: 任意のスキルリポジトリ。プラグインは `plugins/<plugin>/` 配下、スキルは `plugins/<plugin>/skills/<skill>/SKILL.md`（`skills/` 直下も可）
- frontmatter は YAML。ただし PyYAML に依存しないため、lint のパーサは「`---` 区切りの先頭ブロックを key: value 行として読む」簡易パーサを実装する（ネストは `metadata:` 等の1段のみ対応。リスト値は `[a, b]` インライン形式と `- ` 行形式の両方を受理。**ブロックスカラー `key: |` / `key: >` は後続のインデント行を連結して1つの値として読む** — 実コーパスに description をブロックスカラーで書くスキルが複数実在するため必須）
- 既存スキルとの互換: lint は既存キー（`allowed-tools`, `context`, `user-invocable`, `when_to_use` 等）を**エラーにしない**。規約外キーは warn 扱い（§6 L12）
- **検査対象リポジトリは読み取り専用**。lint/viz は一切書き込まない（baseline 等の生成物も mekiki 側に置く）

## 4. データモデル / スキーマ

### 4.1 frontmatter スキーマ

キーは3層に分かれる。**公式（Agent Skills 標準 https://agentskills.io/specification ）**、
**Claude Code 拡張**、**プロトコル独自**。前2者はランタイムが解釈し、独自キーは lint・viz のみが読む。
公式キーを「非標準」として減点してはならない（L12 の許容集合に含める）。

#### 必須キー（欠落は error）
| キー | 型 | 公式制約 | 本プロトコルの追加制約 |
|---|---|---|---|
| `name` | string | 1〜64字。小文字英数とハイフンのみ。先頭/末尾のハイフン不可、連続ハイフン (`--`) 不可。親ディレクトリ名と一致 | §4.2 の命名正規表現（種別3分類）にも一致 |
| `description` | string | 1〜1024字。何をするか＋いつ使うかを具体語で | 150〜400字（全角換算。`len(str)` でよい）。§4.3 の内容要件 |

#### 公式の任意キー（Agent Skills 標準）
| キー | 型 | 制約・用途 |
|---|---|---|
| `license` | string | ライセンス名または同梱ファイル名。短く保つ |
| `compatibility` | string | 最大500字。必要な製品・システムパッケージ・ネットワーク等。大半のスキルは不要 |
| `metadata` | map | 文字列キー→文字列値の任意マップ。キー名は衝突しないよう固有にする |
| `allowed-tools` | string/list | 確認なしで使えるツール（空白区切り。Experimental）。**権限の付与であり制限ではない**。スキルは自分に広い権限を与えられるため外部由来のスキルは信頼前に中身を読む |

#### Claude Code 拡張キー（任意。2026-07-29 の公式 frontmatter reference 全表）
| キー | 型 | 用途 |
|---|---|---|
| `when_to_use` | string | 発火条件の補足。**description に連結され、合算1,536字までスキル一覧に載る**（超過分は切り詰め＝発火判定に載らない。L2 が合算で検査）。他クライアントは読まないため可搬性の正は description |
| `paths` | string/list | glob に一致するファイルを扱うときだけ自動発火。過剰発火の機械的抑制（§4.3） |
| `disable-model-invocation` | bool | `true` で **Claude の自律起動を止める**（人間の `/<skill>` 明示起動のみ）。副作用のあるスキル（課金・デプロイ・外部送信・不可逆な書き込み）に必須。§4.6 |
| `disallowed-tools` | string/list | スキル実行中プールから外すツール |
| `user-invocable` | bool | `false` で **/ メニューから隠す**だけ。Skill ツール経由の起動は防げない（`disable-model-invocation` とは別物） |
| `context` | string | `fork` でサブエージェント実行（文脈の隔離） |
| `agent` | string | `context: fork` 時のサブエージェント種別 |
| `background` | bool | `false` で fork の結果を同一ターンで待つ（既定はバックグラウンド） |
| `model` / `effort` | string | このスキル実行中だけのモデル / effort 上書き。決定性が要る処理の固定に使える |
| `hooks` | map | **スキル実行中のみ有効な hook**（全イベント対応・終了時に自動解除）。§4.6 |
| `arguments` | string/list | 位置引数の命名。本文の `$name` / `$ARGUMENTS` 置換に対応 |
| `argument-hint` | string | `/` 補完時のヒント表示 |
| `shell` | string | 本文インラインコマンドのシェル（`bash` / `powershell`） |

#### プロトコル独自キー（任意。ランタイムは解釈しない）
| キー | 型 | 許容値 | 必須になる条件 |
|---|---|---|---|
| `user-invocable` | bool | `true`/`false` | description に「他スキルから」「内部スキル」を含む場合 `false` が必須（L11） |
| `status` | string | `active` \| `deprecated` \| `experimental` | 廃止・実験段階のスキル。省略時 `active` とみなす |
| `canonical` | string | `<plugin>:<skill>` 形式 | 他所に同名/同内容スキルが存在する場合、正本側以外に必須 |
| `requires` | list[string] | 存在するスキル名 | §4.6 のリスク語を本文に含む場合、対応ガードの宣言が必須 |
| `depends_on` | list[string] | artifact 名（§4.5） | 前段スキルの成果物ファイルを入力にする場合 |
| `duplicate_of` | string | `<plugin>:<skill>` 形式 | 意図的な複製側に置き、正本を指す（L7 の warn 降格経路） |

`requires` / `depends_on` / `status` / `canonical` / `duplicate_of` は**ランタイム（Claude Code）が解釈しないプロトコル独自キー**であり、lint・viz・監査だけが読む。特に `requires` は宣言してもガードスキルが自動ロードされるわけではない（本文への起動指示を L18 が検査する）。ランタイムに実際に強制させたい発火抑止は `disable-model-invocation: true` を使う。

公式キーの値そのもの（`allowed-tools` の構文、`compatibility` の内容等）は lint では検査しない。

公式の参照実装 `skills-ref validate`（github.com/agentskills/agentskills）は
**"intended for demonstration purposes only" / "not meant to be used in production"** と
明記されているため、**CI の検証をこれに委譲してはならない**。公式制約（`name` 64字上限・
連続ハイフン禁止・先頭末尾ハイフン禁止）を機械検証したい場合は mekiki 側に実装する。
ただし実コーパス167スキルで**違反0件**だったため現時点では未実装（1件も捕まえない規則は
追加しない — 本プロトコルが要求する「重みを稼がない要素は削る」を lint 自身にも適用する）。
`description` に引用符なしの `": "` を含む値（他クライアントの厳格な YAML パーサで
失敗する既知の互換性問題）も同様に実測0件のため未実装。

#### 禁止事項（error）
- `description` 内のライフサイクル語: `【非推奨】` `非推奨` `deprecated` `旧方式` `置き換え済み` → `status: deprecated` へ移すこと
- （2026-07-29 撤回）かつて「発火条件が `when_to_use` にのみ存在する状態」を禁止事項としていたが、`when_to_use` は Claude Code の公式キーに昇格し description に連結されて発火判定に使われる。L2 は合算で発火語を判定し、合算1,536字超（一覧の切り詰め）を warn する。可搬性の正は description（他クライアントは when_to_use を読まない）

### 4.2 命名規約

**機械検証するのは公式（Agent Skills 標準）の制約のみ**（L1）:

| 検査 | 内容 |
|---|---|
| 文字種・形 | `^[a-z0-9]+(-[a-z0-9]+)*$`（小文字英数とハイフン。先頭末尾および連続ハイフンを許さない） |
| 長さ | 1〜64字 |
| 一致 | frontmatter `name` == 親ディレクトリ名 |

- 適用範囲: **新規スキルのみ error。既存スキルは warn**（§8 エッジケース参照。「既存」の判定は lint 初回実行時に生成する baseline ファイルによる）

種別別の型（アクションは `動詞ing-対象`／ナレッジは `knowledge-<領域>`／ユーティリティは名詞3語以内）は
**[SHOULD] とし機械検証しない**。当初は3分類の正規表現を強制していたが実測で破綻していた:

- 指摘された25件は**すべて公式的に有効な名前**（`cloud-ads-account-setup`・`signup-form-setup` 等、
  `<領域>-<対象>-<動作>` 形。コーパスの15%）。改名は既存の参照を壊すため行動可能でない
- 一方104件（62%）は「名詞3語以内」の catch-all（`^[a-z0-9]+(-[a-z0-9]+){0,2}$`）を通っており、
  `app-setup`・`content-page-create` のようなアクションスキルが動名詞要件を回避していた
  ＝**要件として機能していなかった**
- 真に検出したい問題（`name` とディレクトリ名の不一致）は実測0件で、旧実装ではこの分岐にテストも無かった
- 名前は発火の主要因ではない（公式: description が "the primary mechanism agents use to decide
  whether to load a skill"）。強制コストを払う対象として優先度が低い

### 4.3 description 内容要件

次の3要素をこの順で含む（lint は機械検査可能な範囲のみ検証。§6 L2）:
1. **何をするか** — 三人称・機能記述
2. **いつ発火するか** — ユーザーの症状語・依頼語を具体的に列挙（「〜したい」「〜と言った場合」等の引用形を推奨）
3. **いつ発火しないか** — 近傍スキルへの誘導（該当スキルがある場合）

手本: fixture `querying-warehouse` の「症状ベース＋近傍誘導」構造。上限の目安400字（適用元の実測最長は802字で、過長の反例）。

### 4.4 Contract ブロック（SKILL.md 本文の必須セクション）

SKILL.md の最初の `##` 見出しは `## Contract` とし、次の6項目を `- **項目名**:` 形式で含む:

```markdown
## Contract
- **Trigger**: <発火状況を1文。description と矛盾しないこと>
- **Inputs**: <required / optional を分け、各入力の取得方法を明記>
- **Preconditions**: <実行前に真であるべき条件。可能なら検証コマンドを併記>
- **Outputs**: <生成ファイルのパス（§4.5 スキーム）と形式>
- **Postconditions**: <完了判定と検証方法。scripts の検証ゲートがあればそのコマンド>
- **Non-goals**: <やらないこと。近傍スキル・前後工程への委譲を明記>
```

- 6項目すべて必須。severity は二段構え: **`## Contract` 自体が無い既存スキルは warn**（現状 0/165 のため、既存全量を error にしない）。**Contract が存在するのに `Non-goals` が欠落している場合は error**。他項目の欠落は新規 error / 既存 warn。新規スキルは Contract 全体が必須（欠落 error）
- Preconditions/Postconditions は文章ではなく**実行可能なゲート**を推奨（適用元の手本: 読み取り専用の検証 script が違反で exit 1 を返し、「exit 0 になるまで完了しない」）

### 4.5 成果物パス・スキーム

```
<output_base>/<store_or_target>/<skill-name>/{YYYYMMDD}_<slug>/<NN>_<artifact>.<ext>
```

- ディレクトリ名は **ASCII 英数字とハイフンのみ**（日本語ディレクトリ名は error。適用元で ASCII と日本語ディレクトリの混在が実在した）
- オーケストレーション配下の artifact は `NN_` 番号プレフィックスで順序を表す（適用元の手本: `01_〜06_` の連番）
- 数値の Single Source of Truth は1ファイル（例 `metrics.json`）に固定し、後続 artifact は `_ref` 参照のみ。再計算・手転記は禁止（適用元の手本: 数値不変条件の一覧＋突合ゲート script）

### 4.6 リスク階層 → 必須ガード マッピング

本文（SKILL.md）に下表の「検出語」が含まれる場合、frontmatter `requires` に対応ガードの宣言が必要。

ガードスキル名は**組織固有のため `config.json` の `guards` から読む**（コードに埋め込まない。§6 L8）。
下表の「必須ガード」列は適用元コーパスでの設定例。

| リスク階層 | 検出語（正規表現、大文字小文字無視） | 必須ガード | `disable-model-invocation` |
|---|---|---|---|
| 課金発生 | `広告費\|出稿\|課金\|請求\|予算.{0,6}(設定\|投入)\|billing` | `guards.billing`（例 `billing-guard`） | **必須。L20 が warn で検査** |
| 破壊的書込 | `mutation\|delete\|アンインストール\|上書き\|DROP\s\|外部API.{0,10}(write\|書き込み\|送信\|同期)` | `guards.write`（例 `destructive-write-guard`）または write ガード script | 必須（未強制） |
| ブラウザ自動操作 | `browser\|ブラウザ操作\|Playwright\|puppeteer\|スクリーンショット操作` | `guards.browser`（例 `browser-automation-guard`） | — |
| 外部公開 | `公開\|publish\|投稿\|メール送信\|配信` | 事前確認ゲート（Contract の Preconditions に人間確認を明記。組織非依存なので config 不在でも検査） | 必須（未強制） |

**`disable-model-invocation: true` は `requires` と違いランタイムが実際に解釈する**唯一の発火抑止機構
（Claude の自律起動を止め、人間の `/<skill>` 明示起動のみにする）。実測でこのフラグを持つスキルは
167件中4件だった。機械検証は課金のみ（L20）— 外部公開・破壊的書込の検出語は実測で167件中114件＝68%に
当たり、規則として行動可能でないため対象外にした。フラグは programmatic invocation も止めるので
**flow.json の委譲先には付けない**（オーケストレータが `Skill` ツールで呼べなくなる）。

- ガードは「Skill ツールで起動」方式（散文依存）を廃止し、`requires` 宣言＋ハーネス自動ロードへ移行する（根拠: 適用元の実測でガードスキル起動が14日間で1回だった）
- 高リスク操作の標準形は **hook＋スキル内 preflight の二重化**（一部実行環境でプラグインレベル PreToolUse hook が発火しない既知の制約への対策）
- hook は plugin ルートの `hooks/hooks.json` に加えて**スキル frontmatter の `hooks:` でも宣言できる**（全イベント対応・スキル実行中のみ有効・終了時に自動解除）。スキル固有のガードは frontmatter 側に置くとスキルと一緒に配布される。project スキルの frontmatter hook は workspace trust 受諾後にのみ動く点に注意

### 4.7 データ形式の選択表（マスタ・状態の置き場所）

| データの使われ方 | 形式 | 例 |
|---|---|---|
| 全部読んで理解する自由記述 | Markdown | 提案・notes・knowledge |
| 構造化・フルロードで差分検知 | YAML | baseline / current の状態ファイル |
| 大量追記（イベント履歴） | JSONL（append-only） | `*.history.jsonl`, decisions |
| 実行用クエリ | `.sql` ファイル | `references/sql/*.sql` |
| 絞り込み・結合・集計が必要な大量リレーション | SQLite＋問い合わせ script | （現状該当なし。YAML/JSONL で破綻してから移行） |

- **リレーションを markdown 表で表現してモデルに結合させることを禁止**。関係は ID で表現する（ID による関係表現の実例は適用元にある）
- 不変スナップショット（baseline）と可変ビュー（current）を分離し、差分検出は専用スキル/scriptで行う

### 4.8 eval スキーマ（公式 Agent Skills 標準に準拠）

**独自形式を発明しない**（§4.7 がマスタデータに課している規律を eval にも適用する）。
一次ソース: https://agentskills.io/skill-creation/evaluating-skills

全スキル共通。`<skill>/evals/evals.json`:

```json
{
  "skill_name": "<skill-name>",
  "notes": "<任意。運用メモ。追加キーは許容する>",
  "evals": [
    {
      "id": 1,
      "prompt": "<現実的なユーザー依頼文>",
      "expected_output": "<成功状態の人間可読な記述>",
      "files": ["evals/files/input.csv"],
      "assertions": ["検証可能・観測可能・数えられる言明"]
    }
  ]
}
```

- 必須: トップレベル `skill_name`(str) / `evals`(list)、各要素に `id` / `prompt` / `expected_output`
- 任意: `files`(list) / `assertions`(list)。`notes` 等の追加キーは許容（実在スキルが使用）
- `assertions` は**初回実行の後に書く**（走らせるまで「良い」が定義できない）。
  両構成で常に pass する assertion は削除する（スキルの価値を示さない）
- 実行時に生成されるファイル（`grading.json` / `timing.json` / `benchmark.json` / `feedback.json`）は
  手で書かない。ワークスペースは `<skill>-workspace/iteration-N/<eval>/{with_skill,without_skill}/`
- **runner は自作しない** — `skill-creator`（github.com/anthropics/skills）がこのループを自動化する。
  mekiki L13 は**形式検証のみ**を担う
- 旧独自形式 `cases.json` は移行対象（L13 warn）。かつて `kind`/`expect`/`severity` を持つ
  自前スキーマを定義していたが、公式標準と衝突し、公式形式を「非標準」と誤って減点していた

#### 測定方法論（規約は SKILL_PROTOCOL.md §eval）

lint では強制できないが、精度を担保する本体はここにある:

| 対象 | 方法 | 判定 |
|---|---|---|
| 出力品質 | 各ケースを with_skill / without_skill の2回実行 | pass率・時間・トークンの `delta`。スキル無しで十分ならスキルは不要 |
| 発火精度 | 約20クエリ（positive 8-10＋near-miss negative 8-10）× 各3回 | `trigger_rate` 閾値 0.5。train 60% / validation 40% で過学習を防ぎ、validation pass率で最良版を選ぶ |

### 4.9 flow 宣言（オーケストレーション）

他スキルを順に呼ぶ統括スキルは `<orchestrator>/flow.json` を持つ:

```json
{"flow": [
  {"phase": 1, "skill": "<skill-name>", "inputs": ["<外部入力 or artifact>"], "outputs": ["NN_artifact.ext"]}
]}
```

- `phase`: 1始まりの整数、一意・昇順。`skill`: 委譲先スキルのディレクトリ名（bare name）
- `inputs` / `outputs`: artifact のファイル名、または外部入力の識別子（`store_slug` 等）
- viz はこのファイルがあればフロー図を確定描画。無い場合は本文 `### Phase Registry` 表（`| Phase | ... | 委譲先 | Output |`）からの推定にフォールバック（推定である旨を UI に明示）
- 設計書 §10（フェーズ間 artifact の検証）の機械化はこの宣言を入力とする

### 4.10 コンテキスト最小性（サイズ規律）

規約本体は SKILL_PROTOCOL.md §コンテキスト最小性（検問1〜5＝内容の適切性判定）。
機械検証できるのはサイズの代理指標のみ。公式目安は **"under 500 lines and 5,000 tokens"**
の2本立て（agentskills.io 仕様の progressive disclosure: "Instructions (< 5000 tokens recommended)"）:

- SKILL.md **本文**（frontmatter を除く）が **500 行以下**であること
- かつ本文の**推定トークンが 5,000 以下**であること。推定は `core.estimate_tokens()` —
  **CJK 1文字 = 1トークン、非CJK 4文字 = 1トークン**の概算（外部トークナイザに依存しない。§3）
- **行数だけでは日本語コーパスで空振りする**: 実測（167スキル）で本文は1行平均75.8字、
  500行超は1件のみだが本文5,000字以上は93件。トークン規律の追加で L19 の検出は 1件 → 44件になった
- 超過時の指摘は分割先の指示を含む: 必要時ロードの知識 → `references/`、決定的処理 → `scripts/`
- 内容の検問（既知・削除・行動・層・重複）は機械判定不能のため lint 対象外（レビュー担保）。
  根拠・出典は docs/DECISIONS.md

## 5. リポジトリ構成

```
mekiki/
├── main.go                      # サブコマンド振り分け・フラグ解釈・出力
├── config.example.json          # 組織固有設定のテンプレート
├── SKILL_PROTOCOL.ja.md         # 規約（作者が読む）
├── docs/                        # 使い方・本書・判断記録・README 用画像
├── scripts/                     # 保守用ツール: デモ資産生成・README スクリーンショット
├── skills/                      # mekiki が同梱する Agent Skill（§12）
└── internal/
    ├── skill/                   # 探索・frontmatter 解釈・Contract・トークン推定
    ├── config/                  # ガード名と検出語
    ├── lint/                    # 規則 L1〜L25・統括・baseline・スナップショット・diff・SARIF
    ├── scaffold/                # 雛形生成
    └── atlas/                   # ページモデルと埋め込みテンプレート
```

環境ごとに生成し、コミットしないもの: `baseline.json`・`config.json`・Atlas の出力。

## 6. mekiki ルール定義

### CLI

```
mekiki lint  [PATH...] [--format text|json|sarif] [--severity error|warn]
                       [--baseline baseline.json] [--config config.json] [--update-baseline]
                       [--changed [--base REV]]
mekiki new   NAME      [--out DIR] [--type action|knowledge|util]
                       [--risk billing|write|browser|publish] [--config config.json]
mekiki atlas [PATH...] [--out skill-atlas.html] [--config …] [--baseline …]
                       [--base base.json]
mekiki diff  BASE.json HEAD.json [--format text|json|sarif]
```

- PATH 省略時は環境変数 `MEKIKI_TARGET` を見る（未設定はエラー）。PATH はプラグイン群でも
  個別スキルのディレクトリでもよい
- `--severity` は**表示のフィルタのみ**で exit code には影響しない
- `--changed` は変更が触ったスキルに報告を絞り、exit code も一緒に絞る。`--severity` との
  非対称は意図的で、severity での絞り込みが赤いビルドを緑にしてはならないのに対し、持ち込んで
  も触ってもいない error で PR が落ちることこそ、この絞り込みが取り除く対象だから。資産の
  探索も規則の評価も全体に対して行う — 横断規則は1つのスキルだけ見ても参照の重複や壊れた委譲を
  判定できない。指摘が残るのは、触ったスキルのものか、**メッセージが触ったスキルを名指しして
  いる**場合。これにより、その変更が編集していないスキルに与えた損傷が消えない。`--base` の
  既定は `origin/HEAD` で、解決できないときは推測せずフラグ名を挙げてエラーにする。未コミット
  ・未追跡の変更も「この変更」に含める（手元の確認は通常コミット前だから）。スキルのディレク
  トリと git のパスは、シンボリックリンクを解決し相対パスを絶対化してから比較する。これがない
  と `skills/` 指定やリンク越しの一時ディレクトリで資産全体が削除済みに見える
- `--format sarif` は GitHub code scanning 用の SARIF 2.1.0 を出力する。`diff` に付けた
  場合は **added の指摘のみ**を載せるので、継承した積み残しが PR に注記されることはない。
  結果の URI は、対象ファイルが作業ディレクトリ配下にあれば相対パスになる
- exit code: **0** error なし / **1** error あり（表示から除外されていても） / **2** 使用法・実行エラー
- `--config` の既定は `config.json`。**不在でも動作する**（組織固有名に依存する検査だけスキップ）。
  キーは2つ:
  - `profile` — 資産が対象とするランタイム。`claude-code`（既定）か `generic`。数値の閾値と
    L12 の公式キー集合を選ぶ（一覧上限も拡張キーも標準の規則ではなく Claude Code の挙動
    であるため）。未知の名前は note を出して既定にフォールバックする
  - `limits` — `listing_cap`・`max_body_lines`・`max_body_tokens`。指定するとプロファイルの
    値より優先され、0 はその検査の無効化
  - `guards` — リスク種別 → ガードスキル名（L8・L20）
  - `patterns` — L2/L3/L6/L8/L11 の検出語。既定は英日バイリンガル。上書きはキーごとの完全置換で、
    自前のフラグ群を書かない限り大文字小文字を無視する。不正な正規表現は note を出し、
    そのキーのみ既定にフォールバックする
- `--baseline` の既定は `baseline.json`。不在なら全スキルを既存扱いにする
- `atlas --base` は `lint --format json` のスナップショットを受け取り、差分をページ上に
  表示する（新規・削除スキル、スキル別の追加 error、解消件数）。指定しなければ差分データは
  一切載らない。Tier は比較**しない** — スナップショットは Tier を記録しておらず、base 側の
  Tier を再計算するにはページに与えられていないコーパスを lint する必要があるため。
  `skills` リストを持たない旧版のスナップショットでも指摘の差分は取れ、新規・削除スキルが
  出せない理由をページ上の note で述べる（推測しない）
- `mekiki diff` は `--format json` の2スナップショットを比較し、変更が**増やした（added）**／
  **解消した（resolved）**指摘だけを出す。warn はゲートを通過する設計なので、増えた warn を
  可視化することが PR レビューでの実効部分になる。差分キーは **(rule, skill) の多重集合** —
  line と message は実行間で揺れる（件数入りメッセージが「637行」→「641行」になる）ため除外する。
  キーに含めると、変わっていない1件を added＋resolved として誤報する。
  exit 1 は **added に error が含まれるときだけ**

### ルール一覧

severity の「新規/既存」は baseline.json（初回 `--update-baseline` で生成）に載っていないスキルを「新規」とする。

| ID | 内容 | 検出方法 | severity |
|---|---|---|---|
| **L1** | `name` が命名規約（§4.2）に一致し、ディレクトリ名と一致 | 正規表現3種のいずれかに一致するか | 新規 error / 既存 warn |
| **L2** | `description` が150〜400字・発火語・一覧上限 | 文字数は description 単体。発火語パターン（`使用\|使う\|場合\|とき\|依頼\|〜したい\|と言った`）は **description＋when_to_use の合算**で判定（Claude Code は連結して発火判定に使うため）。合算が **1,536字超**なら一覧で切り詰められる旨を warn | 文字数: 新規 error / 既存 warn。発火語欠落・上限超過: warn |
| **L3** | `description` にライフサイクル語が無い | §4.1 禁止語リストとの照合 | error |
| **L4** | `## Contract` が SKILL.md 最初の `##` 見出しとして存在し、6項目を含む | 見出しパース＋ `- **X**:` 6項目の存在確認 | Contract 自体が無い既存スキル: warn。Contract があるのに Non-goals 欠落: error。他項目は新規 error / 既存 warn（§4.4） |
| **L5** | 参照Dirは `references/`（複数形）、コードは `scripts/` のみ | `reference/`, `bin/`, `assets/scripts/`, `references/scripts/` の存在検出 | 新規 error / 既存 warn |
| **L6** | 決定的処理語が本文にあるのに `scripts/` が無い | 検出語: `計算\|閾値\|除算\|÷\|倍以上\|突合\|camelCase\|正規化\|パース\|集計\|変換.{0,6}(規則\|ルール)` を含む行があり、かつ `scripts/` ディレクトリ不在 | warn（誤検知があり得るため。§8 suppress 可） |
| **L7** | 同一内容ファイルの多重コピー | 全プラグイン横断で references/ 配下の md5 を計算し、同一ハッシュが2スキル以上に存在（8KB未満のファイルは除外） | error。warn 降格は2経路: `canonical`/`duplicate_of` 宣言、またはファイル先頭の生成バナー（AUTO-GENERATED 等＋再生成/同期コマンド。適用元の master/ ファンアウト運用と整合） |
| **L8** | リスク語があるのに `requires` ガード宣言が無い | §4.6 の検出語 × frontmatter `requires`。**ガードスキル名は `config.json` の `guards` から読む**（組織固有のためコードに埋めない。未設定のリスク種別は検査しない）。外部公開の事前確認だけは組織非依存なので常に検査 | error |
| **L9** | `scripts/` があるのに test が無い | `scripts/` 内に `.py`/`.sh` があり、`tests/`・`test/`・`test_*.py`・`*_test.sh` がいずれも無い | 新規 error / 既存 warn |
| **L10** | plugins/ 配下のビルド成果物 | 作業ツリー走査（`find plugins -name '*.zip' -o -name '*.tar.gz'` 相当）。**git 追跡有無を問わず検出**（適用元で実在した配布 zip は git 未追跡で、`git ls-files` では検出できなかった） | error |
| **L11** | 内部スキルの `user-invocable: false` 欠落 | description に「他スキルから\|内部スキル」を含むのに `user-invocable: false` が無い | error |
| **L12** | 非標準 frontmatter キーの検出 | §4.1 に無いキーを列挙。公式キー（`when_to_use`・`hooks`・`paths`・`model` 等 2026-07-29 の全表）は対象外 | warn |
| **L13** | eval が公式形式（§4.8） | 検査対象は `evals/evals.json`。公式スキーマ（`skill_name`/`evals[id,prompt,expected_output]`）と照合。旧独自形式 `cases.json` は移行を促す warn、他の別名 JSON は非標準 warn（不在は Tier 情報として報告） | error（不適合時）/ warn（旧形式・別名） |
| **L14** | 同名スキルの複数プラグイン定義 | `skills/<name>` が2プラグイン以上に存在し、いずれにも `canonical` 宣言が無い | error |
| **L15** | `flow.json` のスキーマ適合（オーケストレーション。§4.9） | 存在する場合のみ検査。各要素に `phase`(int)/`skill`(str)/`inputs`(list)/`outputs`(list)、`phase` は一意昇順 | error（不適合時） |
| **L16** | `flow[].skill` が実在スキル | flow の各 `skill` を全スキル名集合と照合（横断） | error |
| **L17** | フロー順序整合 | ある input が別フェーズの output と同名なのに、それを生成するフェーズが同一以降 | warn |
| **L18** | `requires` に宣言したガードが本文に登場しない | requires の各要素が SKILL.md 本文に文字列として含まれるか（宣言はランタイムに解釈されず、それだけではガードは起動しない。本文の命令形起動指示を担保する） | warn（言及の有無しか機械判定できないため） |
| **L20** | 副作用スキルに `disable-model-invocation: true` が無い（§4.6。横断） | 課金の検出語 × `disable-model-invocation`。**除外**: flow.json の委譲先（フラグは programmatic invocation を止めるため付けるとオーケストレータが呼べなくなる）、`config.guards` に列挙されたガードスキル自身（リスクを説明するが実行しない）。外部公開・破壊的書込は検出語が実測68%に当たるため対象外 | warn（ヒューリスティック。§8 suppress 可） |
| **L19** | SKILL.md 本文のサイズ規律（§4.10） | frontmatter を除く本文が **500行超** または **推定トークン 5,000超**（CJK 1字=1.2トークン概算）。指摘に references/・scripts/ への分割指示を含む | warn（サイズは「必要な情報だけか」の代理指標にすぎないため。§8 suppress 可） |
| **L21** | `requires` / `depends_on` の参照先スキルが存在しない（横断） | 発見済みの全スキル名および `plugin:name` キーと照合。`config.guards` のガード名は存在扱い（L8 が同じ宣言を要求するため、両立不能にしてはならない） | warn（資産の一部だけを lint した場合、他プラグインへの正当な参照が解決できないため） |
| **L25** | eval の実行結果 | `evals/results.json` を読む。`evals[{id, pass}]` と `queries{total, correct}` はどちらも任意だが最低1つは必要。不在は無言。発火精度は記録するが合否判定はしない | 形式不適合: error。失敗ケースあり: warn。どちらも Tier 3 を阻む |
| **L24** | あるフェーズの output をどの後続フェーズも消費しない | `flow.json` の outputs を後続フェーズの inputs と照合。最終フェーズは対象外（その output はワークフローの成果物） | warn（途中のフェーズが分岐の終端になることは正当にあり得るため） |
| **L23** | 2つ以上のスキルが同じ引用トリガー句を主張している（横断） | description ＋ `when_to_use` 中の引用句（`「」`・`『』`・`"`・`“”`）を4ルーン以上で比較。語が空白で区切られる言語では1語のみの句を除外し、`${` を含む句も除外する。グループ内に `canonical`/`duplicate_of` 宣言があれば黙る | warn（発火判定はモデル側にあり、これは証拠であって証明ではないため） |
| **L22** | スキル自身のディレクトリへの Markdown リンクが解決しない | リンク先が `references/`・`scripts/`・`assets/`・`evals/` 配下のもののみ。アンカーは除去し、ディレクトリも解決とみなす。本文中の**単なるパス言及は検査しない** —— 実測では例示か他スキルのファイルであり、ローカルファイルの主張ではなかった | warn（`references/` のファイルは生成物で未コミットの場合があるため） |

### suppress 記法
誤検知の抑制は SKILL.md 内コメントで行う。理由の記載が無い suppress は無効。
```markdown
<!-- mekiki: disable L6 -- 判断基準の説明であり実計算はscripts/calc.pyが行う -->
```

## 7. 処理フロー

### lint
1. PATH を解決し、`SKILL.md` を持つディレクトリを列挙
2. 各スキル: frontmatter パース → L1〜L4, L8, L11, L12 を評価
3. 各スキル: ディレクトリ走査 → L5, L6, L9, L13 を評価
4. 横断パス: 全 references/ の md5 集計 → L7、スキル名の横断集計 → L14、`git ls-files` → L10
5. suppress コメントを findings に適用
6. findings を severity 降順・スキル名順に整列し、`--format` に従い出力。exit code を決定

### new
```
mekiki new NAME [--out DIR] [--type action|knowledge|util]
                [--risk billing|write|browser|publish] [--config PATH]
```
1. `<skill-name>` を**公式 name 制約**（§4.2）で事前検証し、予約語 `anthropic`/`claude` も弾く。
   不適合は生成拒否（exit 1）。既存ディレクトリにも上書きしない（exit 1）
2. `<out>/<skill-name>/` に生成:
   - `SKILL.md` — frontmatter＋`## Contract`（6項目）＋`## 進め方`＋`## Gotchas`。埋める箇所は `[TODO]`
   - `evals/evals.json`（公式・出力品質）と `evals/eval_queries.json`（公式・発火精度）の雛形
   - `references/.gitkeep`
3. `--type knowledge` は `user-invocable: false` を付ける（L11）
4. `--risk` 指定時:
   - `config.json` の `guards.<risk>` から**ガード名を読んで** `requires` と**本文の命令形起動指示**を入れる
     （L18。宣言だけでは起動しないため本文側が実効部分）。未設定ならコメントの `[TODO]` として残し、
     **ガード名を捏造しない**
   - `billing` / `publish`（不可逆な副作用）は `disable-model-invocation: true` を付ける（L20）
5. 残った `[TODO]` の数と、次に埋めるべき対象を標準出力に提示する

生成物は `description` 未記入（L2 warn）以外の finding が出ないことをテストで担保する
（`tests/test_scaffold.py`。雛形自身が規約違反を作らないことの回帰）。

## 8. エッジケース・エラー処理

| ケース | 挙動 |
|---|---|
| frontmatter が無い / `---` が閉じない | L0 エラーとして error 1件を報告し、そのスキルの他ルールはスキップ |
| 簡易パーサで読めない複雑な YAML（多段ネスト等） | 読めたキーのみで評価し、warn「frontmatter を完全解釈できない」を追加 |
| ブロックスカラー（`description: \|` / `>`） | 後続インデント行を連結して1つの値として評価（§3。連結後の文字列で L2/L3 を判定し、誤検知させない） |
| baseline.json 不在（初回実行） | **全スキルを「既存」として扱う**（warn 側に倒し、初回の error 洪水を防ぐ）。警告「baseline がありません。`--update-baseline` で生成してください」を出力 |
| symlink されたスキル | 実体を1回だけ評価（realpath で重複排除）。L7 の md5 集計も realpath 単位 |
| 8KB 未満の references 重複（L7） | 検出対象外（テンプレ断片の偶然一致を除外するため） |
| 既存/新規の判定 | baseline.json に無い name は新規。baseline は `--update-baseline` でのみ更新（レビュー付き PR で更新する運用） |
| L6 の誤検知 | suppress 記法（§6）。抑制理由必須 |
| 同一スキルへの重複 finding | (rule, file, line) で一意化 |
| スキルゼロのプラグイン | スキャン対象外としてスキップ（エラーにしない） |

## 9. 受け入れ基準

### 9.1 lint の回帰テスト（監査で実測した欠陥をそのままテストケースにする）

`internal/lint` の fixture は**実コーパスで観測された欠陥の縮約コピー**
（名前は中立化済み。実名の対応は適用元の内部記録で管理）。各ルールが検出することを
テストで固定する:

| ルール | fixture（実在欠陥の縮約） | 期待 |
|---|---|---|
| L2 | `enriching-product-page`（description 38字・発火語なし） | error＋warn |
| L3 | `legacy-sheet-setup`（description が `【非推奨】` で開始） | error |
| L5 | `querying-warehouse`（`reference/` 単数形） | warn（既存） |
| L6 | `flash-sale`（除算＋1.3倍閾値の散文、scripts 無し） | warn |
| L7 | 8KB超の共有ナレッジが2プラグインに md5 同一（実在は94KB×17スキル） | error |
| L8 | `paid-ads-setup`（課金操作なのにガード未参照） | error |
| L10 | `legacy-bundle.zip`（git **未追跡**の配布 zip。`git ls-files` では検出不能なことの回帰） | error |
| L11 | `resolving-queue-tasks`（自称「内部」なのに `user-invocable` 未指定） | error |
| L14 | `knowledge-shared-master`（2プラグインに同名・canonical 無し） | error |
| L1 | 合成 fixture（公式 name 制約に反する新規スキル名。例 `MySkill_v2`） | 新規 error |
| L9 | `signup-form-setup`（`scripts/` あり・test 無し） | warn（既存） |
| L12 | `flash-sale`（非標準キー `x-note`。当初 `when_to_use` を使ったが公式昇格した） | warn |
| L13 | 合成 fixture（旧 `cases.json`・公式 `evals.json`・別名 JSON の3系統） | warn / error / warn |
| L21 | 実在する参照先・`plugin:name` 形式・存在しない参照先・config のガード名 | なし / なし / warn / なし |
| L22 | 壊れたリンク・実在ファイル・ディレクトリ・アンカー付き・重複リンク・散文のパス言及 | warn / なし / なし / なし / 重複排除 / なし |
| L23 | 2スキルが共有する句・短い句・英単語1語・`${VAR}` を含む句・canonical 宣言・vendor 重複 | 各 warn / なし / なし / なし / なし / 1件のみ |
| L24 | 誰も読まない中間 output・全て消費されるフロー・最終フェーズの output・壊れた flow.json | warn / なし / なし / なし |
| L25 | results 不在・全件 pass・失敗ケースあり・queries のみ・壊れた JSON・何も測っていない報告 | なし / なし / warn / なし / error / error |
| パーサ | `building-context-from-handover`（description がブロックスカラー） | L2/L3 で誤検知しない |

### 9.2 完了条件
1. 上記 9.1 の全ケースが `go test` で green
2. 数百スキル規模の全量実行が 60 秒以内に完了し、exit code / JSON 出力が仕様どおり
3. 規約を自主的に守っていたプラグインの error 件数が全プラグイン中最少であること（規約が「良い設計」を正しく高評価する妥当性確認）
4. scaffold で生成した新規スキルが、`[TODO]` を埋めた時点で lint error ゼロ
5. suppress 記法が機能し、理由なし suppress が無効化されること
6. 同梱スキル（§12）が無指摘であること。それを満たすために書かれた唯一のスキルが満たせない
   規則は見直すべき規則であり、毎回のテストでこれを検査する

### 9.3 開発手法
t_wada 流 TDD で実装する。§9.1 の表がそのまま初期テストリストである。1ケースずつ失敗するテストに翻訳→実装→リファクタを繰り返し、途中で気づいた検出漏れ・誤検知はテストリストに追加する。


## 11. 非機能要件

- 性能: 約200スキル・references 数千ファイルの全量スキャンで 60 秒以内（md5 計算は 8KB 未満のファイルを除外して抑える）
- 依存: Go 標準ライブラリのみ。ネットワークアクセスなし。read-only（`--update-baseline` 時の baseline.json 書き込みのみ例外）
- 出力の安定性: findings の順序は決定的（severity → skill → rule → line）。CI での diff 比較を可能にする

## 12. 同梱スキル

`skills/` には、このツールを動かす Agent Skill を**場面で分けて**2つ同梱する。
`auditing-skill-corpus` の存在理由は、mekiki が出す指摘文それ自体は読めば分かる一方で、
**作業の順序**は分からないから。エージェントが間違えるのは順序のほうである。継承した資産に
いきなり lint をかけて編集を始める（最初の一手は baseline を決めること）、warn を作業リストと
して扱う、散文ヒューリスティックの指摘を「引っかかった文を消す」ことで直す、ガードが未設定の
ときにガード名を捏造する。`getting-started-with-mekiki` はそれ**以前**の接触を担う——バイナリが
未インストール、ユーザーはどのコーパスの話かも error 件数の意味も知らない——そして運用判断が
始まるところで意図的に止まる。baseline の判断を代行せず、修復もしない。

| パス | 内容 |
|---|---|
| `auditing-skill-corpus/SKILL.md` | 順序そのもの: baseline が先、問いに対応するコマンド、出力の仕分け、決めずに訊くべきこと |
| `auditing-skill-corpus/references/rule-playbook.md` | 規則ごとの修復手順。規則番号順ではなく「修復アクション」別に束ねる |
| `getting-started-with-mekiki/SKILL.md` | 初回接触: インストール、コーパス発見（候補間は確認）、初回 lint と Atlas、平易な要約、次の一手を1つ |
| `*/evals/` | 各スキルに公式2形式。互いの非発火クエリは、もう一方と近傍スキルを狙う |

2つは発火を取り合ってはならないので、引用トリガー句を**互いに素**にし（`skills/` の lint の
たびに L23 が検査する）、双方の description と Non-goals で相手を名指しする——初回は
getting-started、それ以降は auditing。

構造上の注記が2つ。修復手順を `references/` に置いたのは、指摘が手元に来て初めて必要になる知識
だから（§コンテキスト最小性の層テスト）であり、加えて機械的な理由がある。修復手順は L8・L20 が
検出するリスク語彙を引用せざるを得ないが、この2規則が読むのは description と本文だけなので、
本文に置くと**説明している当の規則を自分で踏む**。もう1つ、このスキルが引き受けるのは**機械的
監査だけ**である。「そのスキルの書き方が良いか」という判断はレビュー系スキルの領分で、発火を
取り合わないよう Non-goals で明示的に名指ししている。

**不変条件:** `mekiki lint skills` は無指摘であり、`TestBundledSkillPassesItsOwnLinter` が
それを検査する。規約を満たすために書かれたスキルたちが満たせない新規則は、出荷前に見直す
べき規則である。
