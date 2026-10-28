# Changelog

All notable changes to Dibs. Versions follow [semantic versioning](https://semver.org).

## v0.0.0 (2026-10-28)

The first version: a multiplayer card table for phones, at dibs.mobasity.com.

### Added
- A plain landing page with Create Lobby and Join Lobby.
- Lobbies for 2 to 8 players: create one with a name and a game, share the six-character code or link, and others join from their phones with just a name, no account.
- A lobby room with clockwise seats, teams, ready markers, who's connected, renaming, and owner controls to move players, set teams, change settings, hand over ownership, remove someone or end the lobby.
- Staying in the game through refreshes, app switches, locked phones and lost signal: the browser keeps a private session, reconnects on its own and gets the full current table back.
- Protection against guessing lobby codes: attempts are limited per address, and a client that guesses too often finds no lobbies at all for a while.
- Abandoned lobbies close by themselves: after 2 hours with nobody connected, or 6 hours without any activity.
- Real decks: standard 52, 52 plus jokers, Euchre's 24 (9 to Ace), a 108-card color deck for UNO-style games, several decks combined, or a custom pick of ranks and suits. Every physical card is distinct, even two Aces of Spades from two decks.
- Card handling that works like a real table: shuffle (with a cryptographically secure shuffle), cut at a chosen depth, draw, deal, move, flip, peek, play into piles and slots, swap into a grid, gather everything back, and draw blind from another player's hand when the table allows it.
- Dealing as people really deal: pick the recipients and their order, clockwise or counterclockwise from the dealer's left or right, any number of passes, and packet sizes per player and per pass (such as 2-3 then 3-2), all at once or one packet at a time.
- Offering the cut: the dealer offers a chosen player the cut, and nothing is dealt from that pile until they cut or decline.
- Hidden cards never leave the server: each phone receives only the cards its player may see. Others' hands, face-down cards and the deck's order are sent as plain counts and backs, with nothing that could identify or track a card.
- The table on your phone: shared piles on the felt, the other players' hands as card backs, your own hand and grid at the bottom. Tap a card or pile to see what you can do with it; nothing needs dragging.
- A blank table for improvising any game: the lobby's deck, a discard pile, an open play area, hands, and optional personal grids; the dealer or owner can add more piles and rows of slots during play.
- Dealer and turn markers, passing the deal to the left, and a fresh table with the same seats.
- Next hand in one tap: every card goes back to the deck face down and the deal passes to the left, ready to shuffle and offer the cut again.
- Undo for your own last move when it showed nobody a new card; anything that revealed a card stays done.
- A scoreboard for players or teams, with a target score, quick +1/−1, points with a reason, a history of every change, and one-tap corrections. Scores carry over when the table is reset.
- Three score rules, chosen when the lobby is made: solo (everyone changes only their own score), team (one scorekeeper per team, appointed by the owner), or owner only. The server enforces them on every change.
- A history of everything that happened at the table, naming only cards everyone could see.
- A health endpoint, `/health`, reporting the running version.
- Docker image and compose file for Lighthouse: tests run before every deploy, the container runs read-only as a non-root user, and the site is reached only through Cloudflare.
