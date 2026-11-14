# UK - London preset

The `uk-london` preset contains **110 editable channels**: London BBC One and ITV1, national entertainment and factual services, satellite movie and sport channels, news, and children's television. Its country is `GB`, and its time zone is `Europe/London`.

The preset uses stable Antennarr numbers grouped by category: entertainment 101–161, films 301–312, sport 401–419, news 501–508, and children 601–610. These are **local tuner numbers**, not a promise to reproduce Sky, Freesat, or Plex guide numbering.

For Plex, select the UK and a London postcode such as **SW1A 1AA**, then choose an available guide covering the channels you enable. Match BBC One and ITV1 to their London editions. The exact guide offerings and programme mapping still need real Plex verification; metadata matching tests cannot establish that. Freesat also uses location to choose regional services. [Freesat channel list](https://www.freesat.co.uk/channels?page_b6430=1).

## Matching decisions

- BBC One and ITV1 require explicit London evidence in the name or guide identifier. A generic `BBC1` or `ITV1` alone is not enough, nor is a guide identifier containing only `London`. Dotted and compact network-plus-London identifiers work. Conflicting regional names or identifiers are rejected, including a recognised network followed by an unknown regional suffix.
- BBC Two accepts the England/national service and rejects Wales and Northern Ireland variants. Other channels retain their UK identity; Irish and international variants are not silently substituted.
- Names, punctuation, common abbreviations, quality markers, and case can vary. Programme identities cannot: Discovery is separate from Discovery History, Sky Comedy from Sky Cinema Comedy, and Sky Sci-Fi from Sky Cinema Sci-Fi & Horror.
- Timeshifts, catch-up, loops, event placeholders, and contradictory recognised guide identifiers are excluded. Compact delayed-service IDs such as `legendxtraplus1.uk` also veto a misleading name, even when that sister channel is not selected. A missing roster is preferable to recording the wrong programme.
- Current U& brands accept their established Dave, Drama, Eden, W, Yesterday, Alibi, and Gold names. These are channel renames, not loose keyword matches. [UKTV's channel rebrand](https://corporate.uktv.co.uk/newsroom/uktv-completes-the-rollout-of-new-u-masterbrand-with-pay-channels-uandgold-and-uandalibi).
- Sky One, ITV Quiz, and 5DRAMA use their current identities. Sky Max, Sky Showcase, ITVBe, and 5SELECT are not assumed to carry the replacement service. Seasonal movie pop-ups and individual Sky Sports+ event feeds are omitted. [Sky One announcement](https://skygroup.sky/en-gb/article/iconic-tv-channel-sky-one-is-coming-back1), [ITV Quiz announcement](https://www.itv.com/presscentre/presscentre/media-releases/itv-launches-brand-new-channel-itv-quiz), [Freeview's current channel list](https://www.freeview.co.uk/corporate/platform-management/channel-listings-industry-professionals).

UK `Syfy` and `Racing UK` are accepted as exact older names for Sky Sci-Fi and Racing TV. Their renames are confirmed by [Sky](https://www.skygroup.sky/article/syfy-channel-re-brands-to-sky-sci-fi-offering-new-shows-and-fan-favourites) and [Racecourse Media Group](https://racecoursemediagroup.com/news/racinguk_to_become_racingtv/). Bare `Racing`, Racing TV Extra, and foreign Syfy services remain separate.

Every entry's editable aliases and exclusions are in [`uk_london_channels.json`](../internal/catalog/data/uk_london_channels.json). Region requirements supplement the negative keywords; they do not rely on an exhaustive list of every wrong local edition. Additional regional, sister-channel, and former service identifiers in [`uk_reference_channels.json`](../internal/catalog/data/uk_reference_channels.json) supply conflict vetoes only; they never create channels or assign feeds.

Updated preset aliases apply when adding or resetting a preset. Existing edited lineups are preserved. The matching revision refresh rechecks saved assignments once: conflicting roster places are removed with an event, without adding account failures, while valid roster places remain. A manually configured delayed channel such as `Legend Xtra +1` can still be matched to its own guide identifier.

## Provider metadata audit

The preset was checked against **22 distinct provider catalogues**, captured on 11 November 2026: **258,303 labels and guide identifiers** in total. All 22 lists were reviewed, including lists with little UK coverage and contradictory country metadata. This is not a claim that all 22 offer complete, independent UK lineups; several share the same naming patterns.

The follow-up matching review after alpha.2 still finds 13,220 entries with UK name or guide evidence. Of those, 3,101 are unambiguous matches to this preset, covering **103 of 110 channels across 17 catalogues**. These figures describe saved metadata, not tested media connections, source rights, or current availability. No provider endpoints, account details, or private catalogue files are included here.

The seven channels without an accepted match in that snapshot remain useful editable definitions: 5DRAMA, 5STAR, GREAT! TV, Legend, GREAT! Action, CNN International, and Nick Jr. Too. They are allowed to have empty rosters. For example:

| Observed metadata | Decision |
| --- | --- |
| BBC One London with a Scotland guide ID | Reject the conflicting region. |
| Generic ITV1 with an Anglia guide ID | Do not use for London. |
| BBC One London with a compact `bbcnorthwest` guide ID | Reject the conflicting region. |
| BBC One with only `london.uk` as its guide ID | Insufficient network identity. |
| GREAT! Movies Action with a `greatmovies` guide ID | Reject the sister-channel conflict. |
| Quest Red with a `questredplus1` identifier | Reject the delayed feed. |
| 5 DRAMA with a `5star` identifier | Reject the sister-channel conflict. |
| Generic CNN with no explicit international identity | Do not assume CNN International. |
| Sky Cinema Sci-Fi identified as the entertainment channel Sky Sci-Fi | Reject the conflicting service. |

<details>
<summary>Coverage by anonymised provider sample</summary>

“Accepted channels” counts distinct preset identities, not feeds. Zero is a valid result when the available labels cannot establish a safe match.

| Sample | Catalogue entries reviewed | Accepted channels |
| --- | ---: | ---: |
| 01 | 18,091 | 101 |
| 02 | 7,535 | 2 |
| 03 | 9,188 | 101 |
| 04 | 12,234 | 101 |
| 05 | 5,778 | 0 |
| 06 | 9,619 | 101 |
| 07 | 9,482 | 0 |
| 08 | 7,431 | 101 |
| 09 | 4,489 | 37 |
| 10 | 10,383 | 101 |
| 11 | 14,022 | 2 |
| 12 | 6,330 | 0 |
| 13 | 10,330 | 101 |
| 14 | 7,431 | 101 |
| 15 | 28,627 | 101 |
| 16 | 8,211 | 0 |
| 17 | 7,336 | 22 |
| 18 | 24,480 | 101 |
| 19 | 6,903 | 0 |
| 20 | 8,828 | 18 |
| 21 | 12,796 | 101 |
| 22 | 28,779 | 101 |

</details>

<details>
<summary>Preset channels and metadata coverage</summary>

The final column is the number of the 22 saved catalogues with at least one accepted label for that channel. It is not a live source count.

| Number | Channel | Catalogues |
| --- | --- | ---: |
| 101 | BBC One London | 12 |
| 102 | BBC Two | 13 |
| 103 | ITV1 London | 12 |
| 104 | Channel 4 | 13 |
| 105 | 5 | 13 |
| 106 | BBC Three | 12 |
| 107 | BBC Four | 13 |
| 108 | ITV2 | 13 |
| 109 | ITV3 | 13 |
| 110 | ITV4 | 13 |
| 111 | ITV Quiz | 12 |
| 112 | E4 | 14 |
| 113 | E4 Extra | 12 |
| 114 | More4 | 12 |
| 115 | 4seven | 13 |
| 116 | 5ACTION | 13 |
| 117 | 5DRAMA | 0 |
| 118 | 5STAR | 0 |
| 119 | 5USA | 13 |
| 120 | Sky One | 13 |
| 121 | Sky Witness | 12 |
| 122 | Sky Atlantic | 13 |
| 123 | Sky Comedy | 12 |
| 124 | Sky Crime | 12 |
| 125 | Sky Arts | 13 |
| 126 | Sky Sci-Fi | 13 |
| 127 | Sky Mix | 12 |
| 128 | Sky Documentaries | 12 |
| 129 | Sky Nature | 12 |
| 130 | Sky History | 13 |
| 131 | U&Dave | 13 |
| 132 | U&Drama | 13 |
| 133 | U&Eden | 13 |
| 134 | U&W | 13 |
| 135 | U&Yesterday | 13 |
| 136 | U&Alibi | 13 |
| 137 | U&Gold | 13 |
| 138 | Comedy Central | 14 |
| 139 | Comedy Central Extra | 13 |
| 140 | MTV | 12 |
| 141 | TLC | 13 |
| 142 | Discovery | 12 |
| 143 | Discovery Science | 13 |
| 144 | Discovery History | 12 |
| 145 | Discovery Turbo | 13 |
| 146 | Investigation Discovery | 13 |
| 147 | Animal Planet | 12 |
| 148 | National Geographic | 12 |
| 149 | Nat Geo Wild | 13 |
| 150 | Crime + Investigation | 13 |
| 151 | DMAX | 12 |
| 152 | Quest | 13 |
| 153 | Quest Red | 1 |
| 154 | Really | 12 |
| 155 | Food Network | 13 |
| 156 | Challenge | 13 |
| 157 | BLAZE | 13 |
| 158 | PBS America | 12 |
| 159 | GREAT! TV | 0 |
| 160 | Legend | 0 |
| 161 | Legend Xtra | 13 |
| 301 | Film4 | 13 |
| 302 | Talking Pictures TV | 13 |
| 303 | GREAT! Action | 0 |
| 304 | Sky Cinema Premiere | 13 |
| 305 | Sky Cinema Action | 13 |
| 306 | Sky Cinema Comedy | 13 |
| 307 | Sky Cinema Drama | 13 |
| 308 | Sky Cinema Family | 13 |
| 309 | Sky Cinema Greats | 13 |
| 310 | Sky Cinema Sci-Fi & Horror | 13 |
| 311 | Sky Cinema Thriller | 13 |
| 312 | Disney+ Cinema | 13 |
| 401 | Sky Sports Main Event | 15 |
| 402 | Sky Sports Premier League | 13 |
| 403 | Sky Sports Football | 13 |
| 404 | Sky Sports Cricket | 13 |
| 405 | Sky Sports Golf | 13 |
| 406 | Sky Sports F1 | 15 |
| 407 | Sky Sports Tennis | 13 |
| 408 | Sky Sports Action | 13 |
| 409 | Sky Sports Mix | 13 |
| 410 | Sky Sports News | 13 |
| 411 | Sky Sports Racing | 13 |
| 412 | Sky Sports+ | 12 |
| 413 | TNT Sports 1 | 13 |
| 414 | TNT Sports 2 | 13 |
| 415 | TNT Sports 3 | 13 |
| 416 | TNT Sports 4 | 13 |
| 417 | Premier Sports 1 | 13 |
| 418 | Premier Sports 2 | 13 |
| 419 | Racing TV | 13 |
| 501 | BBC News | 13 |
| 502 | Sky News | 12 |
| 503 | GB News | 12 |
| 504 | CNBC Europe | 13 |
| 505 | CNN International | 0 |
| 506 | Bloomberg Television Europe | 12 |
| 507 | Al Jazeera English | 12 |
| 508 | BBC Parliament | 13 |
| 601 | CBBC | 13 |
| 602 | CBeebies | 13 |
| 603 | Sky Kids | 12 |
| 604 | Nickelodeon | 13 |
| 605 | Nick Jr. | 12 |
| 606 | Nick Jr. Too | 0 |
| 607 | Nicktoons | 12 |
| 608 | Cartoon Network | 13 |
| 609 | Boomerang | 13 |
| 610 | Cartoonito | 1 |

</details>

## Verification and maintenance

The synthetic suite checks all positive aliases with case, quality, and country decoration, all negative phrases, foreign-country rejection, unique channel numbers, and provider-shaped regression cases. It also runs 23,980 cross-service guide-conflict checks, tests delayed-service identifiers, and verifies that a matching refresh removes unsafe saved roster places without strikes or changes to the US roster. Misleading IDs and BBC/children's shared-feed labels are covered. Tests use only names and synthetic metadata; they do not contact IPTV providers.

Run the focused tests with:

```sh
go test ./internal/catalog -run TestLondon -count=1
```

Review the preset when broadcasters change their lineups. Match a new name to an existing service only when its identity is established. Do not reuse a former channel's guide identifier simply because a provider left it in place.

Additional primary references, checked 13 November 2026:

- [Sky's channel packages](https://www.sky.com/help/articles/tv-channel-list-sky-glass) — satellite/pay-TV scope and service names; channel numbers can vary by platform.
- [WBD's TLC and free-to-air portfolio](https://press.wbd.com/gb/media-release/new-look-free-air-tlc-launches-today?language_content_entity=en).
- [WBD's Eurosport/TNT Sports change](https://press.wbd.com/emea/media-release/tnt-sports-welcome-even-more-live-sport-uk-and-ireland?language_content_entity=en) — do not equate old Eurosport labels with an arbitrary TNT Sports feed.
- [Disney and Sky's expanded agreement](https://press.disney.co.uk/news/sky-and-disney-announce-expanded-multi-year-agreement-making-disneyplud-available-to-millions-more-customers-in-the-uk-ireland) — Disney+ Cinema.
- [Premier Sports account migration](https://www.premiersports.com/us/content/60/faqs-account-migration) — current Premier Sports 1/2 identities.
