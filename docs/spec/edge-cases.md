# Edge cases agentx does not handle specially

agentx is built for realistic skills: folders of Markdown and text files, maybe scripts, images and small subfolders. The cases below get no handling of their own: agentx leaves them to git's own behaviour or fails with a generic error, and loses nothing.

- A skill file with lines of its own that look like conflict markers (`<<<<<<<`, `=======`, `>>>>>>>`): a conflicting `skill update` leaves git's markers at git's default size in the pending merge's worktree, so such a line and a marker look alike there. agentx never reads the markers.
- A file that conflicts for a reason other than its lines, such as a binary file, a file executable on one side only, a symlink meeting a file or a file moved to two places: `skill update` lists it with the words `git status` uses for the versions it has, `both modified` or `added by us` say, and nothing more. `git status` in the worktree says the rest.
