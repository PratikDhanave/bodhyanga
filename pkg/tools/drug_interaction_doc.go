// drug_interaction.go — DrugInteraction tool.
//
// Overview:
//   Hard-coded interaction table covering ~12 canonical pairs (ACE inhibitor
//   + K-sparing diuretic, warfarin + NSAID, statins + macrolides, metformin
//   + iodinated contrast, SSRIs + MAOIs, amiodarone + digoxin, beta-blockers
//   + verapamil, clopidogrel + omeprazole). The tool scans every unordered
//   pair of input drugs against the table and returns the matches with
//   severity/mechanism/management. Outputs carry metadata.source =
//   "demo-synth" so downstream consumers know the data is illustrative.
//
// Key declarations:
//   - DrugInteraction: zero-value Tool implementation
//   - drugInteractionTable: the illustrative pair map (package-private)
//   - keyOf(a, b string) [2]string: canonical lowercase/sorted lookup key
//
// See doc.go for package-level documentation.

package tools
