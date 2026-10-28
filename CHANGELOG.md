# Changelog

All notable changes to Dibs. Versions follow [semantic versioning](https://semver.org).

## v0.0.1 (2026-10-28)

### Changed
- Dibs now listens on port 3180 in production (cloudflared routes `dibs.mobasity.com` to `http://dibs:3180`), and on 3181 for local development, both with `go run` and the Docker dev setup.

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
- Euchre, guided from shuffle to 10 points: partners across the table, the cut offered to the dealer's right, a real two-pass deal from the dealer's left (3-2 or 2-3, or 2s then 3s), the upcard on the kitty, ordering up or calling trump, alone or not, the dealer's pickup and discard, tricks awarded to whoever won them, and the hand scored by the rules (1, 2, 4 for a lone march, 2 for a euchre).
- Texas Hold'em: two hole cards dealt one at a time, burn and flop, turn and river onto a five-card board, a burn pile nobody can look at, folding, showing your cards, and the button and blinds moving each hand.
- Six-card Golf: a face-down 2x3 grid each, flips, draws from the deck or the discard, swaps into your grid, and an optional score by the common count.
- Blackjack: a shoe of one or more decks, the dealer's hole card (only the dealer can peek), hit and stand, collecting the round and reshuffling the discards.
- A game guide on the table: where the hand is, and buttons for the usual next steps. Every button is an ordinary action anyone could take by hand; nothing is forced or automatic.
- A blank table for improvising any game: the lobby's deck, a discard pile, an open play area, hands, and optional personal grids; the dealer or owner can add more piles and rows of slots during play.
- Dealer and turn markers, passing the deal to the left, and a fresh table with the same seats.
- Next hand in one tap: every card goes back to the deck face down and the deal passes to the left, ready to shuffle and offer the cut again.
- Undo for your own last move when it showed nobody a new card; anything that revealed a card stays done.
- A scoreboard for players or teams, with a target score, quick +1/−1, points with a reason, a history of every change, and one-tap corrections. Scores carry over when the table is reset.
- Three score rules, chosen when the lobby is made: solo (everyone changes only their own score), team (one scorekeeper per team, appointed by the owner), or owner only. The server enforces them on every change.
- A history of everything that happened at the table, naming only cards everyone could see.
- Built for phones held upright, from 320 px wide, in light and dark mode, with large tap targets.
- A health endpoint, `/health`, reporting the running version.
- Docker image and compose file for Lighthouse: tests run before every deploy, the container runs read-only as a non-root user, and the site is reached only through Cloudflare.
