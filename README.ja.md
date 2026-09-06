# mekiki（目利き）

**スキル資産に目利きの目を通す。**

English: **[README.md](README.md)**

*mekiki*（目利き）は「品質や真贋をひと目で見極める鑑定眼」を意味する日本語です。
Agent Skills（`SKILL.md`）の資産——ダウンロードしてきたものではなく、自分たちが書いて
運用しているもの——に対して、その目を通す単一バイナリです。

**何を解決するか:** Agent Skills の公開標準が定めるのはスキル1つの書式まで。mekiki は
スキル資産全体の品質ゲートです。数十のスキルを一緒に運用したときに現れる問題——コピー
されて乖離していく共有知識、発火しない・し過ぎる description、宣言だけで起動されない
ガード、存在しないスキルへ委譲するオーケストレーター——を機械的に検出し、資産全体を
1枚のページで見せます。

| コマンド | 何をするか |
|---|---|
| `mekiki lint` | 25の番号付き規則で資産を検査する。散文に置き去りにされた計算処理、宣言されているだけで起動されないガード、同じ依頼を2つのスキルが取り合っている状態まで |
| `mekiki atlas` | 資産全体を**自己完結の HTML 1枚**に描画する。ボード／各ワークフローを歩けるパイプライン／並べ替え可能な一覧 |
| `mekiki new` | 最初から規約準拠の雛形を生成する。Contract・Gotchas・公式2形式の eval 入り |
| `mekiki diff` | PR が**増やした**指摘と**解消した**指摘だけを報告する。レビューが見るべきは差分で、積み残しではない |

Go 標準ライブラリ以外の依存なし。設定ファイル無しで動作。検査対象は読み取りのみ。

## 30秒で

```bash
go install github.com/toritori0318/mekiki@latest
mekiki lint path/to/skills
```

```
note: no baseline at baseline.json; treating every skill as pre-existing. Run `mekiki lint <path> --update-baseline` to create one.
error L8   acme-toolkit:publishing-deck     …/publishing-deck/SKILL.md:1     external publication wording ("Publish") is present but Contract Preconditions states no human confirmation step
error L16  acme-toolkit:orchestrating-launch  …/orchestrating-launch/flow.json:1  flow references skill "archiving-run", which does not exist
warn  L18  acme-toolkit:querying-warehouse  …/querying-warehouse/SKILL.md:2  requires: declares "guard-billing" but the body never mentions it (a declaration alone starts no guard — write an imperative invocation step)
warn  L9   acme-toolkit:publishing-deck     …/publishing-deck/scripts:1      scripts/ contains executable code but ships no test

2 errors, 4 warnings (9 skills)
```

各指摘は *深刻度・規則ID・スキル・ファイル:行・何が問題か* の順で読めます。exit code は
error が無ければ **0**、あれば **1**。warn でビルドは落ちません。

そのうえで、資産全体を1枚で眺めます:

```bash
mekiki atlas path/to/skills && open skill-atlas.html
```

## 規模が大きくなると何が壊れるか

[Agent Skills 公開標準](https://agentskills.io) はスキルの**書式**を定めます。しかし、
組織で数十〜数百のスキルを運用し始めたときに起きることには答えを持ちません:

- 計算・閾値判定・ペイロード変換といった決定的な処理が、散文のまま確率的なモデルに委ねられる
- 共有知識がスキル間で物理コピーされ、静かにドリフトする
- 発火が description の書きぶり次第になり、出ない・遅れる・一斉に出る
- 課金や破壊的操作のガードが「宣言はされているが起動されない」状態になる
- オーケストレーターが、どこにもインストールされていないフェーズに委譲している
- そして、それらが正しく動いていることを示す手段が無い

mekiki はこの層を埋めます。規約は165スキルの実監査から導き、すべての規約に規則番号を対応させ、
**規則は実コーパスで測ってから採用**しています。

## Atlas

`mekiki atlas` が書き出すのは **HTML 1ファイル**だけです。サーバー不要、ネットワークアクセス
なし、読む側に何もインストールさせません。PR に添付しても、作業中ずっと開いておいても構いません。
同じ資産に対する3つのビューがあり、それぞれ答える問いが違います。

*以下のスクリーンショットは合成のデモ資産です。写っているスキル名・説明はすべて架空のものです。*

### Board — 手元に何があるのか

プラグインごとに並ぶ名前入りタイル。資産の規模になると点は読めませんが、名前は読めます。
各タイルに成熟度 tier・lint 件数・scripts や evals の有無。workflow スキルには印が付き、
そのまま Flow ビューへ飛べます。上部のゲージには**カタログの常時コスト**——「これらのスキルが
存在する」と知っているためだけに毎セッション支払っているトークン——も並びます。

![Board ビュー: 6プラグイン192スキル。各タイルに成熟度 tier・lint 件数・資産](docs/images/atlas-board.png)

### Flow — 何が、どの順で、何を渡して動くのか

対象は workflow スキルだけ。1本のパイプラインを、パン・ズーム・カードの移動ができる
キャンバスに描きます。フェーズごとに1枚のカード、そして線は**成果物を追って**引かれます——
`01_ctx.json` を消費するフェーズは、直前のフェーズではなく**それを産んだフェーズ**と結ばれます。

![Flow ビュー: 6フェーズのパイプライン。線のラベルは受け渡す成果物](docs/images/atlas-flow.png)

キャンバスの下の **Runbook** は、同じパイプラインを上から読み下せる形です——説明の全文、
Contract の起動条件、各フェーズが受け取るもの・産むもの、どのフェーズに何を渡すか、そして
**Non-goals**（「その処理はどこでやっているのか」と思ったとき、たいていこれが答えになります）。
ボタン1つで全体を Markdown としてコピーできます。隣の**成果物台帳**は、どこで産まれて誰が
消費するかを並べ、どの上流フェーズも産んでいない入力に印を付けます。**資産内に存在しない
委譲先**は赤で描かれます。パイプラインが壊れる、まさにその位置に。

![Runbook: フェーズごとの説明・起動条件・入出力・受け渡し・Non-goals](docs/images/atlas-runbook.png)

`flow.json` で呼び出し順を宣言していれば、そのままここに現れます。無い場合は本文の
`### Phase Registry` 表から推測し、**推測であることを明示**します——持っていない知識を
持っているかのようには見せません。

### Catalog — どのスキルに手を入れるべきか

並べ替え可能な一覧表。tier、Contract の充足を6つのドットで、依存の入出力、資産、error、warn。
`deps in` で並べ替えればハブが、`err` で並べ替えれば明日やる仕事が見つかります（開いた直後は
この順です）。

![Catalog ビュー: error 件数の多い順。tier・Contract の充足・依存件数・資産を1行に](docs/images/atlas-catalog.png)

どのビューでもクリックすれば同じ dossier が開き、指摘と推移的な依存トレースが読めます。

## PR の手前に置く

素のゲートは `mekiki lint --severity error`。引き継いだ資産で効くのは差分のほうです——base と
head のスナップショットを取り、**その変更が増やした分だけ**で落とします。

```bash
mekiki lint base/skills --format json > base.json
mekiki lint head/skills --format json > head.json
mekiki diff base.json head.json     # exit 1 は error が増えたときだけ
```

どちらのコマンドにも `--format sarif` を付けられます。GitHub はこれを該当行のインライン注記
として描画するので、ジョブログの本文を読みに行かずに済みます。Atlas に base のスナップショットを
渡せば（`mekiki atlas path/to/skills --base base.json`）、その変更が何を増やし・解消し・
持ち込んだかがページ上に印されます。差分を資産全体の文脈の中で読むための形です。

コピペできる GitHub Actions の設定と、積み残しを返さずに導入する方法は
**[docs/USAGE.ja.md](docs/USAGE.ja.md)** にあります。

## 新しいスキルを始める

```bash
mekiki new drafting-weekly-plan --out path/to/skills
```

雛形には Contract・Gotchas・公式2形式の eval が入ります。そして「そもそも作るべきではない
かもしれない」という注意も一緒に出ます:

```
Before you fill this in: a new skill is a last resort. If an existing skill's Gotchas or
description, a line in the agent instructions, or a paths/hooks setting would do, delete
this and grow the existing skill instead — the always-on catalog cost scales with the
number of skills.
```

## エージェントから使う

`skills/` には、場面の違う2つの Agent Skill を同梱しています:

- **`getting-started-with-mekiki`** — ゼロ知識の入口。「スキルの調子を見たい」と言うだけで、
  バイナリのインストール、コーパスの発見（候補が複数なら推測せず確認）、初回の lint と
  Atlas、結果の平易な説明までやります。指摘の全量を貼らず、baseline の判断も勝手にしません。
- **`auditing-skill-corpus`** — 運用のワークフロー。指摘を1件も読む前に baseline を決める、
  継承した積み残しではなく差分でゲートする、規則が名指しした層で直す（引っかかった文を
  消して済ませない）、ガード名は**捏造せず訊く**。

```bash
cp -r skills/* ~/.claude/skills/
```

どちらも、教えている規約に自分自身が従っています —— `mekiki lint skills` は無指摘、Tier は
両方 T3、そしてその状態を守るテストが本リポジトリにあります。

## 規約

骨格は4つの考え方で、それぞれ mekiki が検査できる規則に対応しています:

- **決定性境界** — 計算・閾値判定・変換は `scripts/`＋test に置く。散文には判断・統合・仮説を残す
- **Contract** — 本文冒頭に Trigger / Inputs / Preconditions / Outputs / Postconditions /
  **Non-goals**。スコープ越境を防ぐのは Non-goals
- **コンテキスト最小性** — 本文はコンテキスト窓の中で他のすべてと注意を争う。だから新設は最後の手段
- **安全の二層防御** — リスクのあるスキルはガードを宣言し、**かつ本文で起動を指示する**
  （宣言だけでは何も起動しない）

成熟度 Tier は同じ規則群の上に積み上がります。SLSA の保証レベルと同型で、tier は願望ではなく
**測られた位置**です。

| Tier | 状態 | 条件 |
|---|---|---|
| **T0** | 存在する | 基本（名前・description・状態語）に error があるか、Contract が未充足 |
| **T1** | 宣言できている | 基本が綺麗で、**かつ** Contract の6項目が揃っている（新規スキルの最低線） |
| **T2** | 安全 | 危ない書き方が残っていない: 散文の計算処理なし・ガードを本文で起動・scripts にテスト |
| **T3** | 実証済み | 公式2形式の eval が両方あり（出力品質＋発火精度）、実行を記録しているならそれが通っている |

該当するものが無い場合は「満たしている」と数えます。守るものが無い知識スキルが T2 に届くのは、
守るものが無い状態が安全側だからです。同じ凡例は Atlas の **? legend** ボタンにも入っています。

## ドキュメント

| ドキュメント | 内容 |
|---|---|
| **[SKILL_PROTOCOL.ja.md](SKILL_PROTOCOL.ja.md)** ([English](SKILL_PROTOCOL.md)) | 規約の全文。スキルの作者が読むもの |
| **[docs/USAGE.ja.md](docs/USAGE.ja.md)** ([English](docs/USAGE.md)) | 全コマンド・CI の設定・baseline・設定・suppress |
| **[docs/DESIGN.ja.md](docs/DESIGN.ja.md)** ([English](docs/DESIGN.md)) | 規則の厳密な定義とデータモデル |
| **[docs/DECISIONS.ja.md](docs/DECISIONS.ja.md)** ([English](docs/DECISIONS.md)) | 何を採用し、何を却下し、なぜそうしたか |

## 開発

```bash
go test ./...
gofmt -l . && go vet ./...
```

規則は**実コーパスで測ってから**出します。大半のスキルに出る警告は無視されるだけで、1件も
捕まえない規則はコストだけを生みます。どちらも実際に起きており、その記録は
[docs/DECISIONS.ja.md](docs/DECISIONS.ja.md) に、規則を変えるときの手順は
[docs/DESIGN.ja.md の §9.3](docs/DESIGN.ja.md#93-開発手法) にあります。上のスクリーンショットは実資産では
なく架空のデモ資産から再生成しています（手順は [scripts/README.md](scripts/README.md)）。

## ライセンス

[MIT](LICENSE)
