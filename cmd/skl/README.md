# skl

dotfiles（chezmoi）で管理している Skill を、カテゴリごとに一覧で表示します。

## 使い方

```sh
skl list
```

カテゴリごとの節に、スキル名と用途を並べます。カテゴリが無い Skill や、一覧に無いカテゴリの Skill があると、警告を標準エラーに出します。

## 読み取るもの

- `chezmoi source-path` の下の `dot_agents/skills/*/SKILL.md`
- 各 SKILL.md の frontmatter の `name`、`metadata.category`、`metadata.summary`

```yaml
---
name: example-skill
metadata:
  category: 開発
  summary: この Skill の用途を1行で書く
---
```

## カテゴリ

表示の順番は、`cmd/skl/main.go` の `categories` で決めています（仕様書運用、レビュー・監査、開発、環境・設定、執筆、その他）。カテゴリを増やすときは、`categories` に足してから `mise run install` で入れ直します。
