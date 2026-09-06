# 使い方

[README](../README.ja.md) の実務編です。各コマンドの動かし方、PR の前に mekiki を置く方法、
設定できること。規則の厳密な定義は [DESIGN.ja.md](DESIGN.ja.md)、規約そのものは
[SKILL_PROTOCOL.ja.md](../SKILL_PROTOCOL.ja.md) にあります。

English: **[USAGE.md](USAGE.md)**

## 資産を検査する

```bash
mekiki lint path/to/skills                     # 人が読む形式
mekiki lint path/to/skills --format json       # 機械が読むスナップショット
mekiki lint path/to/skills --severity error    # error だけ表示
```

PATH はプラグイン群でも個別スキルのディレクトリでもよく、複数指定もできます。PATH を省略した
場合は `MEKIKI_TARGET` を見ます。未設定なら**推測せずエラー**にします。

exit code は **0**（error なし）、**1**（error あり）、**2**（使い方の誤り・実行時エラー）。
`--severity` が絞るのは**表示だけ**で exit code は変えません。絞った実行で赤いビルドが
うっかり緑になることはありません。warn でビルドは落ちません — 誤検知があり得る規則は、
マージを止めるのではなくレビューで扱うべきものだからです。

## CI で変更をゲートする

答える問いが違う2つのゲートがあります。

**資産に error があるか？**

```bash
mekiki lint path/to/skills --format json --severity error   # error があれば exit 1
```

**この変更が資産を悪くしたか？** 引き継いだ資産では答えられるのはこちらだけです。素の
ゲートでは、積み残しを全部返すまで全 PR が落ちます。`mekiki diff` は `--format json` の
スナップショット2つを比べます（**ファイルの diff ではなく指摘の diff**）。exit 1 になるのは
**変更が error を増やしたときだけ**。増えたのが warn なら通し、レビューに残します。

```bash
mekiki lint base/skills --format json > base.json
mekiki lint head/skills --format json > head.json
mekiki diff base.json head.json
```

```
added    warn  L6   acme-toolkit:reviewing-report  deterministic logic ("Calculate") is described in prose but there is no scripts/
resolved error L8   acme-toolkit:publishing-deck   external publication wording ("Publish") is present but Contract Preconditions states no human confirmation step

added 1 (0 errors), resolved 1
```

PR では、この2つのスナップショットは「同じリポジトリの2つのコミット」です:

```yaml
# .github/workflows/skills.yml
on: pull_request
jobs:
  skills:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }          # base コミットに到達できる必要がある
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - run: |
          go install github.com/toritori0318/mekiki@latest
          echo "$(go env GOPATH)/bin" >> "$GITHUB_PATH"
      - name: base と head のスナップショット
        run: |
          git worktree add ../base ${{ github.event.pull_request.base.sha }}
          mekiki lint ../base/path/to/skills --format json > base.json
          mekiki lint path/to/skills --format json > head.json
      - name: この PR が増やした分だけで落とす
        run: mekiki diff base.json head.json
```

`mekiki diff --format json` は `added` / `resolved` / summary を返すので、PR コメントに
投稿するならこちらを使います。

**差分を該当行に注記する。** `--format sarif` は同じ指摘を SARIF 2.1.0 で出力します。
GitHub はこれを PR のインライン注記として描画するので、ジョブログを読みに行く必要が
なくなります。`diff` に付けた場合は **added の指摘だけ**が載ります。これが要点で、
継承コーパスの全量 lint をアップロードすると毎回の PR に積み残しが注記されてしまいます。

```yaml
      - name: この PR が増やした分だけ注記する
        run: mekiki diff base.json head.json --format sarif > mekiki.sarif
        continue-on-error: true            # ゲートが落ちてもアップロード段階まで到達させる
      - uses: github/codeql-action/upload-sarif@v3
        with: { sarif_file: mekiki.sarif }
```

ログ中のパスは作業ディレクトリからの相対です。リポジトリルートで実行しないと注記が
どのファイルにも紐づきません。全量に注記したい場合のために `mekiki lint --format sarif`
もあります。

差分のキーは**規則IDとスキル**で、file と line は意図的に含めていません。base を別パスに
置ける理由も、行がずれただけの編集が「1件解消＋1件追加」に見えない理由もこれです。知って
おくべき裏返しが2つあります。同じスキルの同じ規則で1件直して1件持ち込むと差分ゼロになること、
そして1つのスキルに同じ規則の指摘が複数あるとき、件数は正確ですが引用される行は先頭の指摘で、
必ずしも今回増えたものではないことです。

2つのスナップショットは**同じ mekiki バージョン・同じ `config.json`** で取ってください。
また、どちらにも `--severity error` を付けないこと —— フィルタは warn をスナップショットから
落とすので、反対側の warn が全部 added / resolved に見えてしまいます。片側だけ規則セットが
違うと差分の意味が壊れます。`atlas --base` に渡すスナップショットも同様です。

## 既存資産に導入する

導入は「まず全部直す」から始める必要はありません。

```bash
# 現在のスキルを「既存」として凍結し、error 基準を新規スキルにだけ適用する
mekiki lint path/to/skills --update-baseline
```

`baseline.json` は指定した資産のスキル棚卸しを記録するので、環境ごとに生成し共有しません。
`--baseline` の既定値は `baseline.json`。ファイルが無ければ全スキルを既存として扱います。

`--update-baseline` と `diff` は役割が違うので併用します。baseline が決めるのは**深刻度**
（既存スキルの指摘は warn、同じ指摘でも新規スキルなら error）、`diff` が決めるのは
**その変更が責任を負う範囲**です。

## 新しいスキルの雛形を作る

```bash
mekiki new drafting-weekly-plan --out path/to/skills
```

```
created: path/to/skills/drafting-weekly-plan
Before you fill this in: a new skill is a last resort. If an existing skill's Gotchas or
description, a line in the agent instructions, or a paths/hooks setting would do, delete
this and grow the existing skill instead — the always-on catalog cost scales with the
number of skills.
next: fill in the 11 [TODO] markers in SKILL.md (the description drives activation — see
      the three-part order in the template)
      then the two eval files: evals.json (output quality) and eval_queries.json (trigger accuracy)
```

`--type action|knowledge|util` と `--risk billing|write|browser|publish` が雛形を変えます。
リスクのあるスキルには `## Guard` 節が入り、`config.json` のガード名を挙げて「起動せよ」と
書きます — `requires:` の宣言だけでは何も起動しないからです。副作用が不可逆な種別には
`disable-model-invocation` も付きます。その種別のガードが未設定なら、雛形は**名前を捏造せず**
TODO を残します。存在しない名前の宣言は、何も守らない宣言だからです。

## Atlas を描画する

```bash
mekiki atlas path/to/skills --out skill-atlas.html
```

自己完結の HTML 1ファイル。サーバー不要、ネットワークアクセスなし、読む側に何もインストール
させません。既定の出力先が作業ディレクトリなのは意図的です — ページには資産内の全 description
が埋まるため、追跡対象の場所に既定で書き出してはいけないからです。同じ理由で、本リポジトリの
`.gitignore` は `skill-atlas.html` を除外しています。

3つのビューの説明は [README](../README.ja.md#atlas) にあります。

**1つの変更をレビューする。** base のスナップショットを渡すと、ページが差分を明示します。
変更が追加したスキルには `new` バッジ、増えた error には `+N E`、ヘッダには合計行が出ます。
Catalog には並べ替え可能な `Δ err` 列が増えます。

```bash
mekiki lint ../base/path/to/skills --format json > base.json
mekiki atlas path/to/skills --base base.json --out skill-atlas.html
```

これを PR に添付すると、レビュアーは資産全体の文脈の中で変更を見られます（地図の無い指摘
一覧ではなく）。`--base` を付けなければ差分データは一切載らないので、既定の出力は従来と
同じです。

Tier は意図的に比較**しません**。スナップショットは Tier を記録しておらず、base 側の Tier を
再計算するにはページに与えられていないコーパスを lint する必要があるためです。

## エージェントに監査を回させる

```bash
cp -r skills/* ~/.claude/skills/
```

同梱スキルは2つです。`getting-started-with-mekiki` は初回接触——インストール、コーパスの
発見（候補が複数なら推測せず確認）、初回の lint と Atlas、要約の平易な読み下し——を担い、
baseline と修復には意図的に踏み込みません。`auditing-skill-corpus` が運用ワークフローで、
この文書のうち読み飛ばされる部分——**順序**——のために存在します。指摘を読む前に
baseline を決め、継承した積み残しではなく差分でゲートし、規則が名指しした層で直す（引っかかった
文を消して済ませない）、ガード名が無ければ捏造せず訊く。規則ごとの修復手順は同スキルの
`references/rule-playbook.md` にあります。「その規則が何か」ではなく「**どう直すか**」を書いた
唯一の文書です。

引き受けるのは機械的監査だけです。「そのスキルの書き方が良いか」——文章・構成・エージェントが
誤読しないか——はレビュー系スキルの領分で、発火を取り合わないよう Non-goals に明記しています。

## 設定

`config.json` も `baseline.json` も、無くて構いません。

```bash
cp config.example.json config.json
```

`config.json` が持つのは4つです:

- **`guards`** — 課金・破壊的書込・ブラウザ操作を伴うスキルに要求するガードスキル名。
  組織固有なのでコードに埋め込んでいません。埋め込むと、他組織の CI が「そこに存在しない
  スキルの宣言」を要求して必ず落ちます。ガード名が未設定のリスク種別は検査しません。
- **`patterns`** — ヒューリスティック規則の検出語（正規表現）。既定は**英日バイリンガル**なので、
  大半の利用者はここを触る必要がありません。上書きはキー単位の置き換えで、不正な正規表現は
  note を出してそのキーだけ既定に戻します。
- **`profile`** — 資産が対象とするランタイム。既定の `claude-code` は mekiki が従来から
  持っていた挙動そのままで、拡張キーとスキル一覧の1,536字上限を知っています。`generic` は
  Agent Skills 標準だけを検査するので、`when_to_use` のような Claude Code 拡張は非標準として
  報告され（L12）、一覧上限の検査も行いません（L2）。未知の名前は note を出して
  `claude-code` にフォールバックします。
- **`limits`** — 数値の閾値（`listing_cap`・`max_body_lines`・`max_body_tokens`）。省略すると
  プロファイルの値を使い、ここに書けばそちらが優先されます。`0` はその検査の無効化です。
  本文サイズの上限は特定ランタイムではなく公式ガイダンス由来なので、どちらのプロファイルでも
  同じ値です。

## 誤検知を抑制する

理由の記載が無い suppress は無効です — 理由こそが、その例外をレビュー可能にするものだからです。

```markdown
<!-- mekiki: disable L6 -- 判断基準の説明であり、実計算は scripts/calc.py が行う -->
```

## スコープと前提

- **検査対象は読み取り専用**。`lint` と `atlas` は一切書き込みません。生成物は指定した場所にだけ出ます
- mekiki が判定するのは**自分たちのスキル**です。第三者スキルのマルウェアや
  prompt injection の審査は別の問題で、対象外です
- 検出語は英日で同梱。他言語は `patterns` の上書き1つで対応できます。構造検査は言語非依存です
- 規約中の例は、mekiki が最初に適用されたコーパスに由来します。実在のスキル名としてではなく、
  自分の資産の中で見つけるべき「型」として読んでください
