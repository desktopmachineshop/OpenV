package seeds

// The starter pool for the shared demo products (REQ-118).
//
// These fifteen concepts began life as client-side constants in
// frontend/src/utils/randomProduct.ts, where the roller rendered them fresh on
// every click — picking one of several names and one of two vision patterns at
// random. That made them pleasant to read and impossible to vote for: a
// concept that renders under a different name each time is not an entity
// anybody can rank, so the vote control sat disabled on most rolls and the two
// Top 5 boards were empty on a fresh deployment.
//
// Seeding them as real rows fixes both. Each concept gets ONE canonical name
// and one fixed vision, so it is a stable thing with an identity, a vote count
// and a place on a leaderboard — exactly like a product a member publishes.
// From the pool's point of view there is no such thing as a built-in entry:
// these rows are ordinary shared products that happen to have arrived at boot
// rather than through the publish endpoint.
//
// The TypeScript list stays where it is, with two jobs left: the worked
// examples in the agent's invent-a-product brief, and the fallback the roller
// shows when the pool cannot be read. It is deliberately no longer the source
// of what people see and vote on, so the two copies drifting apart costs
// nothing — edit this file to change the pool.

// builtinProduct is one entry of the starter pool, already in the shape the
// domain validates: prose written out in full rather than assembled from
// fragments at render time.
type builtinProduct struct {
	Category    string
	Name        string
	Description string
	Vision      string
	Problem     string
	TargetUsers string
}

// builtinProducts returns the starter pool. Adding an entry here adds it to
// every deployment's pool on the next boot; changing the Name of an existing
// entry publishes it a second time under the new name, because the pool
// dedupes on the normalized name and has no other idea of identity — so
// rename with that in mind, and never to reword one that people have voted on.
func builtinProducts() []builtinProduct {
	return []builtinProduct{
		{
			Category:    "software",
			Name:        "Yesterdaily",
			Description: "A stand-up meeting assistant that writes your daily update from your actual commit history and gently flags the days you did nothing.",
			Vision:      "Yesterdaily becomes the two-minute ritual that makes stand-up honest without making it longer.",
			Problem:     "Stand-up updates are reconstructed from memory at 9:01 a.m., which is why they are equal parts fiction and apology.",
			TargetUsers: "engineers who reconstruct yesterday from browser history thirty seconds before stand-up",
		},
		{
			Category:    "software",
			Name:        "Bailout",
			Description: "A calendar assistant that invents a plausible, escalating excuse and calls you out of any meeting that runs twenty minutes over.",
			Vision:      "Bailout becomes the escape hatch every over-booked calendar quietly needs.",
			Problem:     "Leaving a meeting early takes either courage or a believable emergency, and almost nobody has one on hand at 4pm.",
			TargetUsers: "people whose 3pm quick sync has run twenty minutes over every week since March",
		},
		{
			Category:    "hardware",
			Name:        "Fogoff",
			Description: "A desk-mounted fog machine and status lamp that releases a small dramatic fog cloud when your focus timer starts and glows green when you are interruptible.",
			Vision:      "Fogoff becomes the availability signal readable from across the room, with theatre.",
			Problem:     "Headphones are ambiguous, do-not-disturb statuses are invisible from three desks away, and saying \"I am busy\" out loud all day is exhausting.",
			TargetUsers: "open-plan developers whose deep work ends with a fourth cheerful \"got a sec?\" before lunch",
		},
		{
			Category:    "hardware",
			Name:        "Truce",
			Description: "A thermostat with a voting panel that requires a quorum before anyone can change the temperature and keeps a log of who keeps trying.",
			Vision:      "Truce becomes the end of the thermostat cold war, complete with an audit trail.",
			Problem:     "A thermostat has one setting and many stakeholders, so the coldest-blooded person wins by stealth every single afternoon.",
			TargetUsers: "households where one person wears a fleece indoors and another opens windows in February",
		},
		{
			Category:    "video game",
			Name:        "Krakenpark",
			Description: "A physics puzzle game that has you parallel-park increasingly enormous sea creatures into increasingly small harbours.",
			Vision:      "Krakenpark becomes the game people sink sixty hours into and describe to friends as \"you park a whale\".",
			Problem:     "The store is full of games about saving the world and nearly empty of games about doing a small, strange task perfectly.",
			TargetUsers: "players who bounced off world-saving epics and want to be quietly excellent at reversing a blue whale into a fishing berth",
		},
		{
			Category:    "video game",
			Name:        "Afterclerk",
			Description: "A cozy management game that puts you behind the counter of a permit office for ghosts who are extremely particular about paperwork.",
			Vision:      "Afterclerk becomes the comfort game that makes admin work feel like a warm bath.",
			Problem:     "Management games simulate empires; nobody serves the player who just wants a tidy desk and a satisfied customer.",
			TargetUsers: "people who unwind by sorting other people's paperwork and would prefer the customers to be haunted",
		},
		{
			Category:    "toy",
			Name:        "Mimicorn",
			Description: "A plush axolotl with a hidden microphone that repeats the last thing it heard in a tiny, slightly wrong voice.",
			Vision:      "Mimicorn becomes the toy that gets confiscated at bedtime and smuggled back by breakfast.",
			Problem:     "Talking toys cycle through the same forty phrases until the batteries die, and children find the pattern in an afternoon.",
			TargetUsers: "parents of six-year-olds who have just discovered that repeating everything back is comedy",
		},
		{
			Category:    "toy",
			Name:        "Sulk",
			Description: "A plush storm cloud that glows brighter and rumbles louder the longer it is left abandoned on the floor.",
			Vision:      "Sulk becomes the toy that does the nagging so nobody in the house has to.",
			Problem:     "Tidying up is a nightly argument because toys have no opinion about being left underfoot.",
			TargetUsers: "families who step over the same three abandoned toys nightly on the way to the tidy-up argument",
		},
		{
			Category:    "kitchen appliance",
			Name:        "Toastmood",
			Description: "A countertop toaster that asks how your morning is going and browns the bread to match, from \"pale and hopeful\" to \"carbonised\".",
			Vision:      "Toastmood becomes the appliance that finally admits toast is an emotional decision.",
			Problem:     "Toasters offer a numbered dial that means nothing, so every slice is a gamble placed before anyone is awake enough to gamble.",
			TargetUsers: "people who set the dial to 4, get charcoal, and start every day negotiating with an appliance",
		},
		{
			Category:    "kitchen appliance",
			Name:        "Kevinproof",
			Description: "An office coffee grinder with a fingerprint lock and a bean ledger that grinds only your beans, weighs every gram taken, and posts a weekly leaderboard of who took whose.",
			Vision:      "Kevinproof becomes the appliance that settles the office bean wars with data instead of passive-aggressive sticky notes.",
			Problem:     "The office kitchen runs on an honour system that collapses the moment someone brings in a good single-origin bag, and there is never any evidence — only suspicion and an empty jar.",
			TargetUsers: "coffee-obsessed office workers locked in a passive-aggressive hunt for the perfect brew and for whoever keeps taking their beans",
		},
		{
			Category:    "wearable",
			Name:        "Sighence",
			Description: "A lapel pin that counts your sighs per hour and charts them against your calendar.",
			Vision:      "Sighence becomes the wearable that turns a vaguely terrible week into a chart you can show someone.",
			Problem:     "Wearables obsess over steps and sleep while ignoring the vital signs of office survival.",
			TargetUsers: "knowledge workers who suspect the recurring Thursday 2pm is why they feel like this, and want receipts",
		},
		{
			Category:    "pet tech",
			Name:        "Petition",
			Description: "A paw-operated intercom that lets pets file formal complaints, which arrive as time-stamped voice notes on your phone.",
			Vision:      "Petition becomes the official grievance procedure every household pet has been demanding for centuries.",
			Problem:     "Pets currently communicate by knocking things off tables — an unstructured feedback channel with no audit trail.",
			TargetUsers: "cat owners who already answer out loud when the cat stares at a full bowl and screams",
		},
		{
			Category:    "board game",
			Name:        "Broth Runners",
			Description: "A social deduction board game that has players smuggling soup across a fantasy border while one of them is secretly the customs inspector.",
			Vision:      "Broth Runners becomes the game night that ends in laughter, betrayal, and one strongly worded house rule.",
			Problem:     "Every group has played the same three deduction games to death and now knows exactly how each other lies.",
			TargetUsers: "game groups who can recite each other's tells after six years of the same three deduction games",
		},
		{
			Category:    "garden tech",
			Name:        "Gnomecast",
			Description: "A solar-powered garden gnome that live-narrates squirrel activity in the voice of an increasingly invested sports commentator.",
			Vision:      "Gnomecast becomes the garden ornament people leave the kitchen window open for.",
			Problem:     "Garden pests get treated as a problem to be solved when they are, in fact, the best entertainment on the street.",
			TargetUsers: "gardeners who have named the squirrel that keeps defeating their bird-feeder engineering",
		},
		{
			Category:    "robot",
			Name:        "Crustodian",
			Description: "A palm-sized fridge robot that guards the last slice of pizza and reports, with photographic evidence, exactly who took it.",
			Vision:      "Crustodian becomes the tiny robot that finally brings law to the shared fridge.",
			Problem:     "A name written on a container is only a suggestion: the shared fridge is a lawless place with no witnesses.",
			TargetUsers: "flatshares where the container says DO NOT EAT, in marker, and it gets eaten anyway",
		},
	}
}
