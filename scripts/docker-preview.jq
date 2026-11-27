# GitHub-only previews have no registry manifest. Select only published images.
flatten
| map(select(
    .prerelease and (.draft | not)
    and any(.assets[]?; .name == "IMAGE-DIGEST.txt" and .state == "uploaded")
  ))
| max_by(.published_at).tag_name
