# Channel matching

Channel numbers, display names, and IDs presented to Plex stay independent of upstream sources. Matching normalises case, Unicode/punctuation, country labels, quality suffixes (including attached UHD), and known brand variants. It compares complete service names rather than unrestricted fuzzy substrings, so CNN International, MTV2, and Discovery spin-offs remain distinct.

Other provider names are additional positive aliases. Prefix a word or phrase with `-` (optional following space) to exclude it; exclusions are case-insensitive and apply to source names and provider TVG IDs. Country boundaries are respected. There is no hard-coded US East/West preference; use channel exclusions such as `-west` where wanted. Learned TVG identities may provide alternate names but cannot override explicit exclusions.

Ambiguous matches are left unassigned. Manual enabled/disabled assignments persist through rematching, except that negative rules can remove forbidden enabled assignments. New streams and changed metadata are rematched incrementally; edits to channel rules re-evaluate affected streams against all selected channels so ambiguities are handled consistently. Changing a channel number does not rematch feeds.

Fresh installations include 143 editable US channels. Tests and preset fixtures live beside the catalogue code; the runtime preset is authoritative. Existing installations retain their own channels.
