"""Draft content for the Desktop CNC Mill sample programme.

Fifteen projects in a tree, three levels deep: the mill; its frame, motion,
control, enclosure and safety; and the parts of the first three. Each
child's requirements refine requirements of its parent, so verification
rolls up from the leaves to the mill.
load.py reads PROJECTS and writes it to a workspace.

Everything here is invented for demonstration: the figures are plausible
for a bench-top mill, not taken from a real product.
"""

PROJECTS = []


class Project:
    def __init__(self, code, name, parent, description, owner=None):
        self.code, self.name, self.parent = code, name, parent
        self.description, self.owner = description, owner
        self.artifacts, self.links = [], []
        self._heading = None
        PROJECTS.append(self)

    def _add(self, key, type_, title, body, attrs=None, top=False):
        a = {"key": key, "type": type_, "title": title, "body": body}
        if not top and self._heading:
            a["parent"] = self._heading
        if attrs:
            a["attributes"] = {k: v for k, v in attrs.items() if v}
        self.artifacts.append(a)

    def _link(self, key, type_, targets):
        for t in targets or []:
            self.links.append({"from": key, "to": t, "type": type_})

    def heading(self, key, title):
        self._add(key, "heading", title, "", top=True)
        self._heading = key

    def persona(self, key, title, body):
        self._add(key, "persona", title, body)

    def need(self, key, title, body, priority="must"):
        self._add(key, "user-need", title, body, {"priority": priority})

    def req(self, key, title, body, method="test", priority="must", derives=None, refines=None):
        self._add(key, "requirement", title, body, {"priority": priority, "verification_method": method, "owner": self.owner})
        self._link(key, "derives-from", derives)
        self._link(key, "refines", refines)

    def hazard(self, key, title, body, severity):
        self._add(key, "hazard", title, body, {"severity": severity})

    def design(self, key, title, body, satisfies=None, mitigates=None):
        self._add(key, "design-item", title, body, {"owner": self.owner})
        self._link(key, "satisfies", satisfies)
        self._link(key, "mitigates", mitigates)

    def test(self, key, title, body, verifies=None, validates=None, execution="manual"):
        self._add(key, "test-case", title, body, {"execution_method": execution})
        self._link(key, "verifies", verifies)
        self._link(key, "validates", validates)


# --------------------------------------------------------------------------
# The mill
# --------------------------------------------------------------------------
p = Project(
    "SYS",
    "Desktop CNC Mill",
    None,
    "A three-axis CNC mill that sits on a bench, runs from a wall socket and cuts aluminium and mild steel to "
    "drawing tolerance. The top of the programme: its requirements flow down to the frame, motion, control, "
    "enclosure and safety projects and on to their parts, and their verification rolls back up here.",
)

p.heading("H-PERS", "Personas")
p.persona("P-OWNER", "Workshop owner", "Runs a two-person job shop. Buys the mill to make fixtures and short runs of aluminium parts without sending them out. Judges it on whether parts measure right and whether it earns its bench space.")
p.persona("P-MAKER", "Maker", "Has used a 3D printer and a router and wants to cut metal at home. Works in a garage or spare room, alone, in the evening.")
p.persona("P-TECH", "School technician", "Looks after the machines in a school or university workshop. Supervises students, some of whom have never used a machine tool.")
p.persona("P-SERVICE", "Service technician", "Repairs the mill in the field or by post. Needs to find a fault from the log and swap a part without a full strip-down.")

p.heading("H-NEEDS", "User needs")
p.need("N-PARTS", "Parts that measure right", "As a workshop owner I need the parts the mill cuts to match the drawing in aluminium and mild steel, so that I can use them without rework.")
p.need("N-ROOM", "Runs in an ordinary room", "As a maker I need the mill to run from a normal wall socket and to be quiet and clean enough for a room in a house, so that I can use it where I live.")
p.need("N-SAFE", "Safe for one person, including a student", "As a school technician I need the mill to protect an inexperienced operator from the tool, from flying chips and from unexpected movement, so that I can let students use it.")
p.need("N-SETUP", "From a file to cutting without a specialist", "As a maker I need to go from a CAM program to a cutting machine in a few minutes without learning a controller, so that the machine does not sit unused.")
p.need("N-REPAIR", "I can keep it running myself", "As a workshop owner I need to do the routine maintenance and the common repairs myself, so that a fault does not stop work for a week.", "should")
p.need("N-STATE", "I know what it is doing", "As a school technician I need to see what the machine is doing and to recover a job that stopped, so that a stopped job does not mean a scrapped part.", "should")
p.need("N-BENCH", "Fits on a bench and through a door", "As a maker I need two people to be able to carry the mill through a house door and set it on a bench, so that I do not need a workshop floor.")
p.need("N-HISTORY", "A record of what happened", "As a service technician I need a record of running hours and alarms, so that I can find a fault without being there when it happened.", "could")

p.heading("H-PERF", "Performance")
p.req("R-ENVELOPE", "Work envelope", "The mill shall move the tool through at least 300 mm in X, 180 mm in Y and 200 mm in Z.", "inspection", derives=["N-PARTS"])
p.req("R-ACCURACY", "Positioning accuracy", "The mill shall position the tool within 0.02 mm of the commanded position over any 100 mm of travel on each axis at 20 °C.", derives=["N-PARTS"])
p.req("R-REPEAT", "Repeatability", "The mill shall return to a commanded position within 0.01 mm on each axis over 20 approaches from the same direction.", derives=["N-PARTS"])
p.req("R-REMOVAL-AL", "Removal rate in aluminium", "The mill shall remove 6061 aluminium at 20 cm³/min or more with a 10 mm end mill without stalling the spindle or losing position.", derives=["N-PARTS"])
p.req("R-REMOVAL-ST", "Removal rate in mild steel", "The mill shall remove 1018 mild steel at 4 cm³/min or more with a 6 mm end mill without stalling the spindle or losing position.", priority="should", derives=["N-PARTS"])
p.req("R-FINISH", "Surface finish", "The mill shall leave a surface roughness of Ra 1.6 µm or better on a finishing pass in 6061 aluminium.", priority="should", derives=["N-PARTS"])
p.req("R-RAPID", "Rapid traverse", "The mill shall traverse each axis at 5 m/min or more.", priority="should", derives=["N-SETUP"])

p.heading("H-PHYS", "Physical and power")
p.req("R-MASS", "Mass", "The assembled mill shall weigh no more than 95 kg.", "inspection", derives=["N-BENCH"])
p.req("R-SIZE", "Size", "The assembled mill shall fit within 700 mm width, 600 mm depth and 800 mm height, and shall pass through a door opening 760 mm wide.", "inspection", derives=["N-BENCH"])
p.req("R-SUPPLY", "Electrical supply", "The mill shall operate from a single-phase 230 V 50 Hz supply and shall draw no more than 10 A.", derives=["N-ROOM"])
p.req("R-NOISE", "Noise", "The mill shall emit no more than 75 dB(A) at 1 m while cutting 6061 aluminium with the door closed.", derives=["N-ROOM"])
p.req("R-CONTAIN", "Chips and coolant stay inside", "The mill shall keep chips and coolant inside the enclosure while the door is closed.", "demonstration", derives=["N-ROOM", "N-SAFE"])

p.heading("H-SAFE", "Safety")
p.req("R-ESTOP", "Emergency stop", "The mill shall bring every axis to rest within 200 ms and the spindle to rest within 3 s of the operator pressing the emergency stop.", derives=["N-SAFE"])
p.req("R-DOOR", "Door interlock", "The mill shall stop the spindle and every axis when the door opens during a cycle, and shall refuse to start a cycle while the door is open.", derives=["N-SAFE"])
p.req("R-RESTART", "No restart after a power cut", "The mill shall stay at rest when power returns after an interruption until the operator resets it.", derives=["N-SAFE"])
p.req("R-ZHOLD", "Z axis holds without power", "The mill shall let the Z axis fall no more than 0.5 mm when power is removed.", derives=["N-SAFE"])
p.req("R-LIMITS", "Travel limits", "The mill shall stop each axis before it reaches the mechanical end of its travel.", derives=["N-SAFE"])

p.heading("H-OP", "Operation")
p.req("R-GCODE", "Runs standard G-code", "The mill shall run programs written in RS-274/NGC G-code using linear moves, arcs, work offsets G54 to G59, tool length offsets and the drilling cycles G81 to G83.", derives=["N-SETUP"])
p.req("R-HOME", "Homing", "The mill shall find its home position on all three axes within 60 s of the operator asking for it.", derives=["N-SETUP"])
p.req("R-STATUS", "Shows its state", "The mill shall show the operator the tool position, spindle speed, feed rate and any alarm, updated five times a second or more.", "demonstration", derives=["N-STATE"])
p.req("R-RESUME", "Resume a stopped job", "The mill shall resume a program from the line where a feed hold or a stop interrupted it, without losing position.", priority="should", derives=["N-STATE"])
p.req("R-PROBE", "Tool length measurement", "The mill shall measure the length of a fitted tool within 0.01 mm.", priority="should", derives=["N-SETUP", "N-PARTS"])

p.heading("H-MNT", "Maintenance")
p.req("R-LUBE", "Routine maintenance", "The mill shall run for 40 hours or more between lubrications, and the operator shall be able to lubricate it with the tools supplied.", "demonstration", "should", derives=["N-REPAIR"])
p.req("R-SPINDLE-SWAP", "Spindle replacement", "One person shall be able to replace the spindle within 30 minutes with the tools supplied.", "demonstration", "should", derives=["N-REPAIR"])
p.req("R-LOG", "Running hours and alarms", "The mill shall record its running hours and every alarm with the time it happened, and shall let the operator export the record.", "demonstration", "could", derives=["N-HISTORY", "N-REPAIR"])

p.heading("H-HAZ", "Hazards")
p.hazard("HZ-TOOL", "Contact with the rotating tool", "A hand reaches the cutter while the spindle turns, during setup or when clearing chips. Harm: laceration or amputation of a finger.", "high")
p.hazard("HZ-EJECT", "Ejected chip or broken tool", "A cutter breaks or a part comes loose and leaves the work zone at speed. Harm: eye injury.", "high")
p.hazard("HZ-RESTART", "Movement after power returns", "Power is restored while the operator is reaching into the work zone and a cycle carries on. Harm: laceration or crush.", "high")
p.hazard("HZ-CRUSH", "Crush between the table and the column", "A hand is between a moving axis and the frame at the end of travel. Harm: crushed fingers.", "medium")
p.hazard("HZ-ZDROP", "The Z axis drops", "Power is lost and the head falls under its own weight onto a hand or the part. Harm: crush; damaged tool and part.", "medium")
p.hazard("HZ-SHOCK", "Electric shock", "A fault makes exposed metal live, or coolant reaches mains wiring. Harm: electric shock.", "high")

p.heading("H-DES", "Design")
p.design("D-ARCH", "Three-axis moving-table layout", "A fixed column carries the Z axis and spindle; the table moves in X and Y on a cast base. The frame and structure, the motion, the control, the enclosure and the safety system are each a project under this one.", satisfies=["R-ENVELOPE", "R-SIZE", "R-MASS"])
p.design("D-ENCLOSURE", "Full enclosure with an interlocked door", "Sheet-steel enclosure around the work zone with a polycarbonate window. The door carries a coded interlock switch on two channels.", satisfies=["R-DOOR", "R-CONTAIN", "R-NOISE"], mitigates=["HZ-TOOL", "HZ-EJECT"])
p.design("D-SAFETY-CHAIN", "Two-channel safety chain", "The emergency stop and the door interlock feed a safety relay on two channels. The relay removes the enable from the axis drives and the spindle drive's safe-torque-off input; the firmware is told but is not relied on.", satisfies=["R-ESTOP", "R-DOOR", "R-RESTART"], mitigates=["HZ-TOOL", "HZ-RESTART", "HZ-CRUSH"])
p.design("D-ZBRAKE", "Spring-applied brake on Z", "A brake on the Z ballscrew is held off electrically and applies by spring when power is lost.", satisfies=["R-ZHOLD"], mitigates=["HZ-ZDROP"])
p.design("D-POWER", "One mains inlet, 48 V inside", "A single filtered mains inlet with a protective earth bond to every exposed metal part. A 48 V supply feeds the axis drives; the spindle drive takes mains directly.", satisfies=["R-SUPPLY"], mitigates=["HZ-SHOCK"])
p.design("D-HOST", "Controller in the machine, software on the operator's computer", "The controller runs the motion in real time. The control software runs on the operator's own computer, connects over Ethernet, and sends programs and shows state.", satisfies=["R-GCODE", "R-STATUS", "R-RESUME", "R-LOG"])

p.heading("H-TEST", "Tests")
p.test("T-ACCURACY", "Accuracy and repeatability with a laser interferometer", "Measure each axis at 20 mm steps over its travel at 20 °C, five runs in each direction. Pass: every error over any 100 mm is within 0.02 mm and the spread at each point is within 0.01 mm.", verifies=["R-ACCURACY", "R-REPEAT"])
p.test("T-ENVELOPE", "Travel, mass and size", "Measure the travel of each axis between limits, weigh the assembled mill, and measure it. Carry it through a 760 mm door frame. Pass: every figure is within its requirement.", verifies=["R-ENVELOPE", "R-MASS", "R-SIZE"])
p.test("T-CUT-AL", "Aluminium cutting trial", "Cut a 10 mm wide, 5 mm deep slot in 6061 at 400 mm/min with a 10 mm end mill, then a finishing pass on the wall. Pass: no stall, position check within 0.02 mm afterwards, and Ra 1.6 µm or better on the wall.", verifies=["R-REMOVAL-AL", "R-FINISH"])
p.test("T-CUT-ST", "Steel cutting trial", "Cut a 6 mm wide, 3 mm deep slot in 1018 at 225 mm/min with a 6 mm end mill. Pass: no stall and position check within 0.02 mm afterwards.", verifies=["R-REMOVAL-ST"])
p.test("T-ESTOP", "Emergency stop times", "Press the emergency stop at full rapid on each axis and with the spindle at 24,000 rpm; record with a high-speed camera and the drive traces. Pass: axes at rest within 200 ms and spindle within 3 s, ten times out of ten.", verifies=["R-ESTOP"])
p.test("T-DOOR", "Door interlock test", "Open the door during a cycle; try to start a cycle with the door open; repeat with one interlock channel disconnected. Pass: motion stops, the cycle does not start, and the single fault is reported.", verifies=["R-DOOR"])
p.test("T-POWER", "Power interruption", "Cut the mains during a cycle with the head at mid travel and restore it after 5 s. Pass: Z falls 0.5 mm or less, nothing moves on restoration, and the cycle starts only after a reset.", verifies=["R-RESTART", "R-ZHOLD"])
p.test("T-NOISE", "Noise and supply current", "Measure sound level at 1 m on four sides and the supply current during the aluminium cutting trial with the door closed. Pass: 75 dB(A) or less and 10 A or less.", verifies=["R-NOISE", "R-SUPPLY"])
p.test("T-FIRST-PART", "First part by a new operator", "Five people who have not used the mill each make the sample bracket from the supplied program, with the quick-start guide only. Pass: four of five finish a part within tolerance in 30 minutes without help.", validates=["N-SETUP", "N-STATE"])
p.test("T-CLASSROOM", "Supervised student session", "A technician runs a two-hour session with six students, each cutting one part. Pass: no hazardous event, no bypassed guard, and the technician rates supervision as workable.", validates=["N-SAFE"])


# --------------------------------------------------------------------------
# Frame & structure, and its three parts
# --------------------------------------------------------------------------
p = Project(
    "FRAME",
    "Mill frame & structure",
    "SYS",
    "Everything that holds the tool and the work in place: the substructure, the XY table and the Z axis. Owns "
    "travel, geometry, stiffness and most of the mass, and shares them out to its three parts.",
    owner="Mechanical team",
)
p.heading("H-REQ", "Requirements")
p.req("R-TRAVEL", "Axis travel", "The frame shall give 305 mm of travel in X, 185 mm in Y and 205 mm in Z between limit switches.", "inspection", refines=["SYS.R-ENVELOPE"])
p.req("R-STRAIGHT", "Straightness", "Each axis of the assembled frame shall move straight within 0.008 mm over any 100 mm of travel.", refines=["SYS.R-ACCURACY"])
p.req("R-SQUARE", "Squareness", "Each pair of axes of the assembled frame shall be square within 0.012 mm over 100 mm.", refines=["SYS.R-ACCURACY"])
p.req("R-STIFF", "Stiffness at the tool", "The frame shall deflect no more than 0.01 mm at the tool point under a 150 N load in X, Y or Z.", refines=["SYS.R-REMOVAL-AL", "SYS.R-REMOVAL-ST", "SYS.R-FINISH"])
p.req("R-RESONANCE", "First resonance", "The assembled frame shall have its first natural frequency at 120 Hz or above.", "analysis", "should", refines=["SYS.R-FINISH"])
p.req("R-FRAME-MASS", "Frame mass", "The frame shall weigh no more than 62 kg without motors, spindle or enclosure.", "inspection", refines=["SYS.R-MASS"])
p.req("R-FRAME-SIZE", "Frame size", "The frame shall fit within 560 mm width, 480 mm depth and 700 mm height at every position of its axes.", "inspection", refines=["SYS.R-SIZE"])
p.req("R-LIMIT-MOUNT", "Limit switches at both ends", "Each axis shall carry a limit switch at each end of travel that operates 3 mm or more before the mechanical stop.", "inspection", refines=["SYS.R-LIMITS"])
p.req("R-LUBE-POINTS", "Lubrication", "Every rail carriage and ballscrew nut shall take grease through a nipple the operator can reach without removing a cover, and shall run 40 hours between greasings.", "demonstration", "should", refines=["SYS.R-LUBE"])
p.req("R-SPINDLE-MOUNT", "Spindle mounting", "The frame shall hold a 65 mm spindle cartridge by four screws the operator can reach from the front.", "inspection", "should", refines=["SYS.R-SPINDLE-SWAP"])
p.heading("H-DES", "Design")
p.design("D-LAYOUT", "Fixed column, moving table", "The substructure is a base with a column bolted to it. The XY table rides on the base; the Z axis rides on the column and carries the spindle.", satisfies=["R-TRAVEL", "R-FRAME-SIZE", "R-STIFF"])
p.design("D-BUDGET", "Error and mass budgets", "Straightness, squareness, deflection and mass are each shared between the substructure, the XY table and the Z axis, with a tenth held back for assembly.", satisfies=["R-STRAIGHT", "R-SQUARE", "R-FRAME-MASS", "R-RESONANCE"])
p.heading("H-TEST", "Tests")
p.test("T-GEOMETRY", "Assembled geometry on the surface plate", "Check straightness of each axis against a straightedge and squareness against a granite square with a 0.001 mm indicator, on the assembled frame. Pass: straightness within 0.008 mm and squareness within 0.012 mm per 100 mm.", verifies=["R-STRAIGHT", "R-SQUARE"])
p.test("T-STIFF", "Assembled stiffness", "Load the tool point to 150 N in each direction through a load cell and read deflection. Pass: 0.01 mm or less.", verifies=["R-STIFF"])
p.test("T-MODAL", "Tap test of the assembled frame", "Excite the assembled frame with an instrumented hammer and read the response at the tool point. Pass: first natural frequency at 120 Hz or above.", verifies=["R-RESONANCE"])

p = Project(
    "BASE",
    "Mill substructure",
    "FRAME",
    "The base and the column: what the table and the Z axis are built on. Its flatness and squareness set the limit "
    "on everything above it.",
    owner="Mechanical team",
)
p.heading("H-REQ", "Requirements")
p.req("R-PADS", "Rail mounting pads", "The pads that carry the Y rails shall be flat within 0.005 mm over their 300 mm length and parallel to each other within 0.005 mm.", refines=["FRAME.R-STRAIGHT"])
p.req("R-COLUMN", "Column squareness", "The column's rail face shall be square to the plane of the base pads within 0.006 mm over 100 mm.", refines=["FRAME.R-SQUARE"])
p.req("R-BASE-STIFF", "Substructure stiffness", "The base and column shall account for no more than 0.004 mm of deflection at the tool point under a 150 N load.", refines=["FRAME.R-STIFF"])
p.req("R-BASE-FREQ", "Substructure resonance", "The base and column, with 20 kg in place of the Z axis, shall have their first natural frequency at 180 Hz or above.", "analysis", "should", refines=["FRAME.R-RESONANCE"])
p.req("R-BASE-MASS", "Substructure mass", "The base and column together shall weigh no more than 38 kg.", "inspection", refines=["FRAME.R-FRAME-MASS"])
p.req("R-FOOTPRINT", "Footprint", "The base shall stand on four levelling feet within a footprint of 560 mm by 480 mm.", "inspection", refines=["FRAME.R-FRAME-SIZE"])
p.heading("H-DES", "Design")
p.design("D-BASE", "Polymer-concrete base", "A cast polymer-concrete base with steel inserts for the rail pads, ground in one setting after casting.", satisfies=["R-PADS", "R-BASE-STIFF", "R-BASE-FREQ", "R-BASE-MASS", "R-FOOTPRINT"])
p.design("D-COLUMN", "Stress-relieved steel column", "A welded steel box column, stress-relieved, bolted to the base on three scraped pads so its squareness can be set at assembly.", satisfies=["R-COLUMN", "R-BASE-STIFF"])
p.heading("H-TEST", "Tests")
p.test("T-CMM", "Pads and column on the measuring machine", "Measure the rail pads and the column face on a coordinate measuring machine at 20 °C. Pass: pads flat and parallel within 0.005 mm, column square within 0.006 mm per 100 mm.", verifies=["R-PADS", "R-COLUMN"])
p.test("T-BASE-STIFF", "Substructure stiffness under load", "Load the column at tool height to 150 N with the base clamped at its feet and read deflection. Pass: 0.004 mm or less.", verifies=["R-BASE-STIFF"])
p.test("T-BASE-MODAL", "Modal analysis and tap test", "Find the first natural frequency by finite-element analysis and confirm it with an instrumented hammer. Pass: 180 Hz or above by both.", verifies=["R-BASE-FREQ"])
p.test("T-BASE-INSPECT", "Mass and footprint", "Weigh the base and column and measure across the feet. Pass: 38 kg or less, within 560 mm by 480 mm.", verifies=["R-BASE-MASS", "R-FOOTPRINT"])

p = Project(
    "XY",
    "Mill XY table",
    "FRAME",
    "The table, the saddle under it, and the rails and ballscrews of the X and Y axes.",
    owner="Mechanical team",
)
p.heading("H-REQ", "Requirements")
p.req("R-XY-TRAVEL", "X and Y travel", "The table shall travel 305 mm in X and 185 mm in Y between limit switches.", "inspection", refines=["FRAME.R-TRAVEL"])
p.req("R-XY-STRAIGHT", "X and Y straightness", "The table shall move straight within 0.006 mm over any 100 mm of X or Y travel.", refines=["FRAME.R-STRAIGHT"])
p.req("R-XY-SQUARE", "X square to Y", "The X axis shall be square to the Y axis within 0.008 mm over 100 mm.", refines=["FRAME.R-SQUARE"])
p.req("R-XY-STIFF", "Table stiffness", "The table and saddle shall account for no more than 0.003 mm of deflection at the tool point under a 150 N load.", refines=["FRAME.R-STIFF"])
p.req("R-XY-MASS", "Table mass", "The table, saddle, rails and ballscrews of X and Y shall weigh no more than 16 kg.", "inspection", refines=["FRAME.R-FRAME-MASS"])
p.req("R-XY-LIMITS", "X and Y limit switches", "The X and Y axes shall each carry a limit switch at each end that operates 3 mm or more before the mechanical stop.", "inspection", refines=["FRAME.R-LIMIT-MOUNT"])
p.req("R-XY-LUBE", "X and Y lubrication", "Every X and Y carriage and ballscrew nut shall take grease through a nipple reachable with the table at mid travel.", "demonstration", "should", refines=["FRAME.R-LUBE-POINTS"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-PINCH", "Pinch at the way covers", "A finger is caught where a way cover folds as the table reaches the end of travel. Harm: bruising or a cut.", "low")
p.heading("H-DES", "Design")
p.design("D-TABLE", "Cast-iron table on a saddle", "A cast-iron table with three T-slots on a cast saddle. X rails sit on the saddle, Y rails on the base.", satisfies=["R-XY-TRAVEL", "R-XY-STIFF", "R-XY-MASS"])
p.design("D-XY-RAILS", "Preloaded profile rails", "Size 15 profile rails with two preloaded carriages per rail, each with a grease nipple facing the front.", satisfies=["R-XY-STRAIGHT", "R-XY-SQUARE", "R-XY-LUBE"])
p.design("D-COVERS", "Way covers with finger clearance", "Bellows covers that keep 25 mm clear at full compression, with the limit switches under them.", satisfies=["R-XY-LIMITS"], mitigates=["HZ-PINCH"])
p.heading("H-TEST", "Tests")
p.test("T-XY-GEOMETRY", "Table geometry", "Indicate a straightedge and a granite square on the table over full X and Y travel. Pass: straight within 0.006 mm and square within 0.008 mm per 100 mm.", verifies=["R-XY-STRAIGHT", "R-XY-SQUARE"])
p.test("T-XY-STIFF", "Table stiffness under load", "Load the table centre to 150 N in X, Y and Z with the saddle at mid travel. Pass: 0.003 mm or less.", verifies=["R-XY-STIFF"])
p.test("T-XY-INSPECT", "Travel, limits and mass", "Measure travel between switches and the gap to each hard stop; weigh the assembly. Pass: every figure within its requirement.", verifies=["R-XY-TRAVEL", "R-XY-LIMITS", "R-XY-MASS"])
p.test("T-XY-GREASE", "Greasing the table", "An operator greases every X and Y point from the manual with the supplied gun. Pass: every point reached with no cover removed.", verifies=["R-XY-LUBE"])

p = Project(
    "ZAX",
    "Mill Z axis",
    "FRAME",
    "The head that carries the spindle, and the rails and ballscrew that move it up and down the column.",
    owner="Mechanical team",
)
p.heading("H-REQ", "Requirements")
p.req("R-Z-TRAVEL", "Z travel", "The head shall travel 205 mm between limit switches.", "inspection", refines=["FRAME.R-TRAVEL"])
p.req("R-Z-STRAIGHT", "Z straightness", "The head shall move straight within 0.006 mm over any 100 mm of travel.", refines=["FRAME.R-STRAIGHT"])
p.req("R-Z-SQUARE", "Z square to the table", "The Z axis shall be square to the table surface within 0.010 mm over 100 mm in both directions.", refines=["FRAME.R-SQUARE"])
p.req("R-Z-STIFF", "Head stiffness", "The head and its rails shall account for no more than 0.003 mm of deflection at the tool point under a 150 N load.", refines=["FRAME.R-STIFF"])
p.req("R-Z-MASS", "Head mass", "The head, its rails and ballscrew shall weigh no more than 8 kg without the spindle.", "inspection", refines=["FRAME.R-FRAME-MASS"])
p.req("R-Z-LIMITS", "Z limit switches", "The Z axis shall carry a limit switch at each end that operates 3 mm or more before the mechanical stop.", "inspection", refines=["FRAME.R-LIMIT-MOUNT"])
p.req("R-Z-LUBE", "Z lubrication", "Every Z carriage and the ballscrew nut shall take grease through a nipple reachable from the front with the head raised.", "demonstration", "should", refines=["FRAME.R-LUBE-POINTS"])
p.req("R-Z-BORE", "Spindle bore", "The head shall clamp a 65 mm spindle cartridge in a bore with four front-facing screws.", "inspection", "should", refines=["FRAME.R-SPINDLE-MOUNT"])
p.heading("H-DES", "Design")
p.design("D-HEAD", "Aluminium head with a split clamp", "A machined aluminium head with a 65 mm split bore closed by four M6 screws from the front.", satisfies=["R-Z-BORE", "R-Z-MASS", "R-Z-STIFF"])
p.design("D-Z-RAILS", "Rails on the column, screw between them", "Two size 15 rails on the column face with the ballscrew on the centre line, so the cutting load does not twist the head.", satisfies=["R-Z-STRAIGHT", "R-Z-SQUARE", "R-Z-TRAVEL", "R-Z-LIMITS", "R-Z-LUBE"])
p.heading("H-TEST", "Tests")
p.test("T-Z-GEOMETRY", "Head geometry", "Indicate a cylinder square on the table while the head travels, front to back and side to side. Pass: straight within 0.006 mm and square within 0.010 mm per 100 mm.", verifies=["R-Z-STRAIGHT", "R-Z-SQUARE"])
p.test("T-Z-STIFF", "Head stiffness under load", "Load a test bar in the bore to 150 N in each direction with the head at mid travel. Pass: 0.003 mm or less.", verifies=["R-Z-STIFF"])
p.test("T-Z-INSPECT", "Travel, limits, mass and bore", "Measure travel and the gap to each stop, weigh the head assembly, and fit a 65 mm gauge in the bore. Pass: every figure within its requirement and the gauge clamps with four screws.", verifies=["R-Z-TRAVEL", "R-Z-LIMITS", "R-Z-MASS", "R-Z-BORE"])
p.test("T-Z-GREASE", "Greasing the Z axis", "An operator greases every Z point from the manual. Pass: every point reached from the front with no cover removed.", verifies=["R-Z-LUBE"])


# --------------------------------------------------------------------------
# Motion, and its four parts
# --------------------------------------------------------------------------
p = Project(
    "MOTION",
    "Mill motion",
    "SYS",
    "Everything that moves: the motor and drive of each axis, and the spindle and its drive. Owns speed, thrust, "
    "backlash, stopping and cutting power.",
    owner="Motion team",
)
p.heading("H-REQ", "Requirements")
p.req("R-BACKLASH", "Backlash", "Each axis shall have 0.005 mm of backlash or less at the table.", refines=["SYS.R-REPEAT", "SYS.R-ACCURACY"])
p.req("R-RESOLUTION", "Resolution", "Each axis shall move in steps of 0.001 mm or less.", "analysis", refines=["SYS.R-ACCURACY"])
p.req("R-AXIS-SPEED", "Axis speed", "Each axis shall reach 5.5 m/min carrying its full load.", refines=["SYS.R-RAPID"])
p.req("R-THRUST", "Axis thrust", "Each axis shall push with 400 N or more continuously at feeds up to 2 m/min.", refines=["SYS.R-REMOVAL-AL", "SYS.R-REMOVAL-ST"])
p.req("R-AXIS-STOP", "Axis stopping", "Each axis shall come to rest within 150 ms of its drive enable being removed at full rapid.", refines=["SYS.R-ESTOP", "SYS.R-DOOR"])
p.req("R-Z-HOLD", "Z holds without power", "The Z axis shall fall no more than 0.5 mm when power is removed.", refines=["SYS.R-ZHOLD"])
p.req("R-CUT-POWER", "Cutting power", "The spindle shall deliver 1.1 kW or more continuously at 12,000 rpm and hold any speed from 2,000 to 24,000 rpm.", refines=["SYS.R-REMOVAL-AL", "SYS.R-REMOVAL-ST"])
p.req("R-RUNOUT", "Tool runout", "The tool shall run out no more than 0.005 mm at the spindle nose.", refines=["SYS.R-FINISH", "SYS.R-ACCURACY"])
p.req("R-SPINDLE-STOP", "Spindle stopping", "The spindle shall come to rest from 24,000 rpm within 2.5 s of its safe-torque-off input opening.", refines=["SYS.R-ESTOP", "SYS.R-DOOR"])
p.req("R-MOTION-CURRENT", "Supply current", "The axis drives and the spindle drive together shall draw no more than 8.5 A from a 230 V supply.", refines=["SYS.R-SUPPLY"])
p.req("R-MOTION-NOISE", "Noise", "The spindle and the axes shall emit no more than 68 dB(A) at 1 m running free with no enclosure.", refines=["SYS.R-NOISE"])
p.req("R-SWAP", "Spindle replacement", "One person shall be able to replace the spindle cartridge within 20 minutes.", "demonstration", "should", refines=["SYS.R-SPINDLE-SWAP"])
p.heading("H-DES", "Design")
p.design("D-AXES", "Closed-loop steppers on ground ballscrews", "Each axis has a closed-loop stepper motor coupled directly to a 12 mm ground ballscrew of 4 mm lead with a preloaded double nut, and a drive fed from the 48 V bus.", satisfies=["R-BACKLASH", "R-RESOLUTION", "R-AXIS-SPEED", "R-THRUST", "R-AXIS-STOP"])
p.design("D-SPINDLE", "Bought-in cartridge spindle", "A brushless cartridge spindle and its drive, bought from a supplier to the requirements of this project.", satisfies=["R-CUT-POWER", "R-RUNOUT", "R-SPINDLE-STOP", "R-SWAP", "R-MOTION-NOISE"])
p.design("D-POWER-BUDGET", "Current budget", "2.5 A of mains current for the 48 V supply feeding three axis drives, 6 A for the spindle drive.", satisfies=["R-MOTION-CURRENT", "R-Z-HOLD"])
p.heading("H-TEST", "Tests")
p.test("T-MOTION-CURRENT", "Supply current with everything moving", "Measure mains current with all three axes at rapid and the spindle at full load on the dynamometer. Pass: 8.5 A or less.", verifies=["R-MOTION-CURRENT"])


def axis_drive(code, axis, load, load_text, thrust, extra=None):
    p = Project(
        code,
        f"Mill {axis} axis motor & drive",
        "MOTION",
        f"The motor, coupling, ballscrew nut and drive of the {axis} axis, which moves {load_text}.",
        owner="Motion team",
    )
    p.heading("H-REQ", "Requirements")
    p.req("R-BACKLASH", f"{axis} backlash", f"The {axis} axis shall have 0.005 mm of backlash or less, measured at the driven part.", refines=["MOTION.R-BACKLASH"])
    p.req("R-RESOLUTION", f"{axis} resolution", f"The {axis} drive shall move the axis in steps of 0.001 mm or less.", "analysis", refines=["MOTION.R-RESOLUTION"])
    p.req("R-SPEED", f"{axis} speed", f"The {axis} axis shall reach 5.5 m/min carrying {load} kg.", refines=["MOTION.R-AXIS-SPEED"])
    p.req("R-THRUST", f"{axis} thrust", f"The {axis} axis shall push with {thrust} N or more continuously at feeds up to 2 m/min.", refines=["MOTION.R-THRUST"])
    p.req("R-STOP", f"{axis} stopping", f"The {axis} axis shall come to rest within 150 ms of its drive enable being removed at 5.5 m/min carrying {load} kg.", refines=["MOTION.R-AXIS-STOP"])
    p.req("R-CURRENT", f"{axis} drive current", f"The {axis} drive shall draw no more than 3 A from the 48 V bus.", refines=["MOTION.R-MOTION-CURRENT"])
    p.req("R-FOLLOW", f"{axis} following error", f"The {axis} drive shall report a fault to the control within 10 ms of the axis falling 0.05 mm behind its commanded position.", refines=["MOTION.R-BACKLASH"])
    if extra:
        extra(p)
    p.heading("H-DES", "Design")
    p.design("D-MOTOR", f"{axis} motor and coupling", "A closed-loop stepper motor with a 4,000-count encoder, joined to the ballscrew by a bellows coupling with no backlash.", satisfies=["R-RESOLUTION", "R-SPEED", "R-THRUST", "R-FOLLOW"])
    p.design("D-NUT", f"{axis} ballscrew nut", "A preloaded double nut on a 12 mm ground ballscrew of 4 mm lead, with fixed and floating bearing supports.", satisfies=["R-BACKLASH"])
    p.design("D-DRIVE", f"{axis} drive", "A 48 V drive with a hardware enable input that removes motor current and shorts the windings to brake the axis.", satisfies=["R-STOP", "R-CURRENT"])
    p.heading("H-TEST", "Tests")
    p.test("T-BACKLASH", f"{axis} backlash by reversal", "Approach a point from both directions ten times with an indicator on the driven part. Pass: mean difference 0.005 mm or less.", verifies=["R-BACKLASH"])
    p.test("T-RESOLUTION", f"{axis} resolution by calculation", "Work out the step size from the encoder count and the screw lead, and confirm ten single steps with a 0.0001 mm indicator. Pass: 0.001 mm or less.", verifies=["R-RESOLUTION"])
    p.test("T-RUN", f"{axis} speed, thrust and current", f"Run 50 cycles at 5.5 m/min carrying {load} kg, then push a load cell at 2 m/min, logging bus current. Pass: no fault, {thrust} N or more, 3 A or less.", verifies=["R-SPEED", "R-THRUST", "R-CURRENT"])
    p.test("T-STOP", f"{axis} stop and fault", "Remove the enable at 5.5 m/min and time the stop; then hold the axis and time the fault report. Pass: at rest within 150 ms; fault within 10 ms of 0.05 mm.", verifies=["R-STOP", "R-FOLLOW"])
    return p


axis_drive("XDRV", "X", 20, "the table and the work on it", 400)
axis_drive("YDRV", "Y", 36, "the saddle, the table and the work on it", 400)


def z_extras(p):
    p.req("R-BRAKE", "Z brake", "The Z brake shall hold the head within 50 ms of power being removed and shall let it fall no more than 0.5 mm.", refines=["MOTION.R-Z-HOLD"])
    p.heading("H-HAZ", "Hazards")
    p.hazard("HZ-BRAKE-WEAR", "Worn brake", "The brake lining wears until it no longer holds the head. Harm: the head falls on a hand or the part when power is removed.", "medium")


z = axis_drive("ZDRV", "Z", 12, "the head and the spindle", 500, z_extras)
z.heading("H-BRAKE", "Brake")
z.design("D-BRAKE", "Spring-applied brake with a wear check", "A spring-applied brake on the Z ballscrew, held off electrically. At each homing the control drives against it at low torque and raises an alarm if the head moves.", satisfies=["R-BRAKE"], mitigates=["HZ-BRAKE-WEAR"])
z.test("T-BRAKE", "Z brake hold", "Cut power 20 times with the head at mid travel and the heaviest tool fitted, recording the drop with an indicator. Pass: held within 50 ms and 0.5 mm every time.", verifies=["R-BRAKE"])

p = Project(
    "SPIN",
    "Mill spindle & drive",
    "MOTION",
    "The spindle cartridge and its drive, built by a supplier to the mill's requirements. The supplier works in "
    "this project and sees the motion requirements it refines, and nothing else of the programme.",
    owner="Northfield Spindles (supplier)",
)
p.heading("H-REQ", "Requirements")
p.req("R-SPEED", "Speed range", "The spindle shall run at any speed from 2,000 to 24,000 rpm, held within 2 % of the commanded speed.", refines=["MOTION.R-CUT-POWER"])
p.req("R-POWER", "Continuous power", "The spindle shall deliver 1.1 kW or more continuously at 12,000 rpm.", refines=["MOTION.R-CUT-POWER"])
p.req("R-TAPER-RUNOUT", "Taper runout", "The spindle taper shall run out no more than 0.003 mm.", refines=["MOTION.R-RUNOUT"])
p.req("R-COLLET", "Tool holding", "The spindle shall hold tools with shanks from 1 mm to 13 mm in ER20 collets.", "inspection", refines=["MOTION.R-CUT-POWER", "MOTION.R-RUNOUT"])
p.req("R-STOP", "Stopping time", "The drive shall bring the spindle from 24,000 rpm to rest within 2.5 s of its safe-torque-off input opening.", refines=["MOTION.R-SPINDLE-STOP"])
p.req("R-STO", "Safe torque off", "The drive shall remove torque from the motor when either of its two safe-torque-off channels opens, and shall report a disagreement between the channels.", refines=["MOTION.R-SPINDLE-STOP"])
p.req("R-CURRENT", "Supply current", "The drive shall draw no more than 6 A from a 230 V supply at full load.", refines=["MOTION.R-MOTION-CURRENT"])
p.req("R-SPIN-NOISE", "Noise", "The spindle shall emit no more than 68 dB(A) at 1 m running free at 24,000 rpm.", refines=["MOTION.R-MOTION-NOISE"])
p.req("R-THERMAL", "Temperature rise", "The spindle nose shall rise no more than 30 K above ambient after one hour at 18,000 rpm.", priority="should", refines=["MOTION.R-RUNOUT"])
p.req("R-CARTRIDGE", "Cartridge replacement", "One person shall be able to replace the spindle cartridge within 20 minutes by removing four screws and one connector.", "demonstration", "should", refines=["MOTION.R-SWAP"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-OVERSPEED", "Overspeed", "A drive fault runs the spindle above the rated speed of the tool. Harm: tool bursts.", "high")
p.hazard("HZ-HOT", "Hot spindle nose", "The operator touches the spindle nose when changing a tool after a long run. Harm: minor burn.", "low")
p.heading("H-DES", "Design")
p.design("D-CARTRIDGE", "Brushless cartridge spindle", "A brushless motor on the spindle shaft, running in two pairs of angular-contact bearings greased for life, in a 65 mm cartridge.", satisfies=["R-SPEED", "R-POWER", "R-TAPER-RUNOUT", "R-COLLET", "R-CARTRIDGE", "R-SPIN-NOISE"])
p.design("D-DRIVE", "Drive with safe torque off and a brake resistor", "A sensorless vector drive with a two-channel safe-torque-off input, a brake resistor for fast stops, and a speed limit set in hardware.", satisfies=["R-STOP", "R-STO", "R-CURRENT"], mitigates=["HZ-OVERSPEED"])
p.design("D-COOLING", "Shaft-driven fan and a finned housing", "A fan on the shaft draws air over a finned housing; a thermistor at the front bearing reports temperature.", satisfies=["R-THERMAL"], mitigates=["HZ-HOT"])
p.heading("H-TEST", "Tests")
p.test("T-DYNO", "Power and current on the dynamometer", "Load the spindle to 1.1 kW at 12,000 rpm for 30 minutes and record speed, shaft power and supply current. Pass: power held, speed within 2 %, current 6 A or less.", verifies=["R-POWER", "R-SPEED", "R-CURRENT"])
p.test("T-RUNOUT", "Runout with a test bar", "Indicate a ground test bar in the taper at the nose and 50 mm out, at four collet positions. Pass: 0.003 mm or less at the nose.", verifies=["R-TAPER-RUNOUT", "R-COLLET"])
p.test("T-STOP", "Stop on safe torque off", "Open each safe-torque-off channel in turn, then both, at 24,000 rpm. Pass: at rest within 2.5 s each time, and a single open channel is reported.", verifies=["R-STOP", "R-STO"])
p.test("T-SOAK", "Thermal soak and noise", "Run at 18,000 rpm for one hour, logging nose temperature; then measure sound level at 24,000 rpm. Pass: rise 30 K or less and 68 dB(A) or less.", verifies=["R-THERMAL", "R-SPIN-NOISE"])
p.test("T-SWAP", "Cartridge swap", "A technician who has not done it before replaces the cartridge from the service sheet. Pass: 20 minutes or less, and runout within limit afterwards.", verifies=["R-CARTRIDGE"])


# --------------------------------------------------------------------------
# Control, and its two parts
# --------------------------------------------------------------------------
p = Project(
    "CTRL",
    "Mill control",
    "SYS",
    "What turns a program into motion and tells the operator what is happening: the control electronics in the "
    "machine and the control software, which is the firmware on the board and the application on the operator's "
    "computer.",
    owner="Controls team",
)
p.heading("H-REQ", "Requirements")
p.req("R-STOP-PATH", "Acting on a stop", "The control shall remove the enable from every axis drive and open the spindle drive's safe-torque-off input within 20 ms of the safety system demanding a stop.", refines=["SYS.R-ESTOP", "SYS.R-DOOR"])
p.req("R-NO-RESTART", "Nothing moves until reset", "The control shall keep every drive disabled after power returns, and after any stop, until the operator resets it.", refines=["SYS.R-RESTART"])
p.req("R-BRAKE-RELEASE", "Z brake release", "The control shall release the Z brake only while the drives are enabled.", refines=["SYS.R-ZHOLD"])
p.req("R-PROGRAM", "Programs it runs", "The control shall run RS-274/NGC programs using linear moves, arcs, work offsets G54 to G59, tool length offsets and the drilling cycles G81 to G83, and shall reject a program it cannot run before the tool moves.", refines=["SYS.R-GCODE"])
p.req("R-PATH", "Path accuracy", "The control shall move the three axes together along a programmed path with a path error of 0.005 mm or less at feeds up to 2 m/min.", refines=["SYS.R-ACCURACY", "SYS.R-GCODE"])
p.req("R-HOMING", "Homing", "The control shall home the three axes within 45 s.", refines=["SYS.R-HOME"])
p.req("R-TRAVEL-GUARD", "Staying inside the travel", "The control shall refuse a move that would leave the travel of an axis, and shall stop an axis within 1 mm of its limit switch operating.", refines=["SYS.R-LIMITS"])
p.req("R-OPERATOR-VIEW", "What the operator sees", "The control shall show tool position, spindle speed, feed rate, machine state and any alarm with its cause, updated five times a second or more.", "demonstration", refines=["SYS.R-STATUS"])
p.req("R-HOLD", "Hold and resume", "The control shall stop on a feed hold without losing position and shall resume the program from the interrupted line.", priority="should", refines=["SYS.R-RESUME"])
p.req("R-TOOL-LENGTH", "Tool length", "The control shall measure a fitted tool's length within 0.01 mm and store it without the operator typing a number.", priority="should", refines=["SYS.R-PROBE"])
p.req("R-RECORD", "Record of use", "The control shall keep the running hours and the last 10,000 events through a power cut and shall export them as a file.", "demonstration", "could", refines=["SYS.R-LOG"])
p.req("R-CTRL-CURRENT", "Supply current", "The control electronics shall draw no more than 1 A from a 230 V supply, not counting the drives.", refines=["SYS.R-SUPPLY"])
p.heading("H-DES", "Design")
p.design("D-SPLIT", "Real time in the machine, the rest on the operator's computer", "The control electronics and the firmware on them run the motion in real time. The application on the operator's computer loads programs, shows state and sends commands over Ethernet. The machine finishes a move safely if the computer goes away.", satisfies=["R-PROGRAM", "R-PATH", "R-OPERATOR-VIEW", "R-HOLD", "R-RECORD", "R-TOOL-LENGTH"])
p.design("D-ENABLE", "Enable in hardware, not in code", "The drive enable and the spindle's safe-torque-off line are switched by hardware that the safety system drives directly. The firmware is told of a stop but is not in its path.", satisfies=["R-STOP-PATH", "R-NO-RESTART", "R-BRAKE-RELEASE"])
p.heading("H-TEST", "Tests")
p.test("T-CONTOUR", "Contouring on the machine", "Cut a 50 mm circle and a 50 mm diamond at 2 m/min and measure with a ballbar. Pass: path error 0.005 mm or less.", verifies=["R-PATH"])
p.test("T-CTRL-SESSION", "A working session", "Home, load a program, set offsets, probe a tool, run with five feed holds and one stop, resume, and export the record. Pass: home within 45 s, every hold and resume keeps position, the tool length is stored, and the record holds every event.", verifies=["R-HOMING", "R-HOLD", "R-TOOL-LENGTH", "R-RECORD", "R-OPERATOR-VIEW"])

p = Project(
    "ELEC",
    "Mill control electronics",
    "CTRL",
    "The controller board, its supplies and the cabinet they sit in: the outputs to the drives, the inputs from the "
    "switches and the probe, and the hardware that removes the drive enable.",
    owner="Electronics team",
)
p.heading("H-REQ", "Requirements")
p.req("R-ENABLE", "Enable line", "The electronics shall remove the axis drive enable and open the spindle safe-torque-off line within 20 ms of the stop input from the safety system opening, with no firmware in the path.", refines=["CTRL.R-STOP-PATH"])
p.req("R-WATCHDOG", "Hardware watchdog", "The electronics shall remove the drive enable when the firmware has not serviced the watchdog for 10 ms.", refines=["CTRL.R-STOP-PATH"])
p.req("R-POWER-UP", "Disabled at power-up", "The electronics shall hold the drive enable off from power-up until the reset input is pressed.", refines=["CTRL.R-NO-RESTART"])
p.req("R-BRAKE-DRIVER", "Brake driver", "The electronics shall power the Z brake coil only from the switched drive-enable rail.", "inspection", refines=["CTRL.R-BRAKE-RELEASE"])
p.req("R-STEP-OUT", "Step outputs", "The electronics shall give step and direction outputs for four axes at up to 250 kHz with edges faster than 100 ns.", refines=["CTRL.R-PATH"])
p.req("R-INPUTS", "Limit, home and probe inputs", "The electronics shall give two filtered inputs per axis and one probe input, each timed to within 20 µs of the signal changing.", refines=["CTRL.R-TRAVEL-GUARD", "CTRL.R-HOMING", "CTRL.R-TOOL-LENGTH"])
p.req("R-ETHERNET", "Link to the operator's computer", "The electronics shall give a 100BASE-TX Ethernet port isolated from the machine's supplies.", "inspection", refines=["CTRL.R-OPERATOR-VIEW", "CTRL.R-PROGRAM"])
p.req("R-STORE", "Non-volatile store", "The electronics shall keep 10,000 events and the hour counter for ten years without power.", "analysis", "could", refines=["CTRL.R-RECORD"])
p.req("R-ELEC-CURRENT", "Supply current", "The board and its supplies shall draw no more than 1 A from a 230 V supply.", refines=["CTRL.R-CTRL-CURRENT"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-COOLANT", "Coolant in the electronics", "Coolant mist reaches the controller. Harm: electric shock or fire.", "medium")
p.heading("H-DES", "Design")
p.design("D-BOARD", "Controller board", "A microcontroller board with step and direction outputs for four axes, timer-capture inputs for limits, home and probe, and an isolated Ethernet port.", satisfies=["R-STEP-OUT", "R-INPUTS", "R-ETHERNET"])
p.design("D-ENABLE-CHAIN", "Enable chain", "The stop input, the watchdog and the reset latch are discrete logic ahead of the enable rail. The brake coil hangs off the same rail.", satisfies=["R-ENABLE", "R-WATCHDOG", "R-POWER-UP", "R-BRAKE-DRIVER"])
p.design("D-FRAM", "Ferroelectric memory", "A ferroelectric memory holds the event ring and the hour counter and needs no battery.", satisfies=["R-STORE"])
p.design("D-CABINET", "Sealed cabinet", "A gasketed cabinet behind the column holds the board and supplies, with mains wiring segregated and a drip loop on every cable.", satisfies=["R-ELEC-CURRENT"], mitigates=["HZ-COOLANT"])
p.heading("H-TEST", "Tests")
p.test("T-ENABLE-TIMING", "Enable timing on the bench", "Open the stop input 20 times and record the enable and safe-torque-off lines on an oscilloscope; then halt the processor. Pass: both lines change within 20 ms of the stop, and within 10 ms of the halt.", verifies=["R-ENABLE", "R-WATCHDOG"])
p.test("T-POWER-CYCLE", "Power cycling", "Interrupt the supply 50 times in every machine state and inspect the brake wiring against the drawing. Pass: enable stays off until reset every time, and the brake coil is on the enable rail.", verifies=["R-POWER-UP", "R-BRAKE-DRIVER"])
p.test("T-SIGNALS", "Outputs and inputs", "Capture step outputs at 250 kHz, and drive each input from a signal generator against a reference clock. Pass: edges under 100 ns, inputs timed within 20 µs.", verifies=["R-STEP-OUT", "R-INPUTS"])
p.test("T-ELEC-INSPECT", "Port, store and current", "Hi-pot the Ethernet isolation, review the memory's data-retention figures, and measure supply current with all outputs active. Pass: isolation holds 1.5 kV, retention ten years or more, 1 A or less.", verifies=["R-ETHERNET", "R-STORE", "R-ELEC-CURRENT"])

p = Project(
    "SW",
    "Mill control software",
    "CTRL",
    "The firmware on the controller board, which reads G-code, plans the motion and makes the step pulses, and the "
    "application on the operator's computer, which loads a program, shows the machine's state and starts the job.",
    owner="Software team",
)
p.heading("H-FW", "Firmware requirements")
p.req("R-PARSE", "G-code it accepts", "The firmware shall accept G0, G1, G2, G3, G17 to G19, G20, G21, G28, G43, G49, G53, G54 to G59, G80 to G83, G90, G91, and M3, M5, M8, M9 and M30, and shall reject any other word with an alarm that names the line.", refines=["CTRL.R-PROGRAM"])
p.req("R-PLAN", "Look-ahead", "The firmware shall plan 32 blocks or more ahead and shall keep the path within 0.003 mm of the programmed corners.", refines=["CTRL.R-PATH"])
p.req("R-JITTER", "Step timing", "The firmware shall place each step pulse within 2 µs of its planned time at step rates up to 250 kHz.", refines=["CTRL.R-PATH"])
p.req("R-SOFT-LIMIT", "Soft limits", "The firmware shall refuse a move that would leave the travel of an axis and shall raise an alarm before it moves.", refines=["CTRL.R-TRAVEL-GUARD"])
p.req("R-HARD-LIMIT", "Limit switch response", "The firmware shall stop stepping an axis within 100 µs of its limit input changing.", refines=["CTRL.R-TRAVEL-GUARD"])
p.req("R-HOME-SEQ", "Homing sequence", "The firmware shall home Z first, then X and Y together, approaching each switch twice, the second time at 100 mm/min.", "demonstration", refines=["CTRL.R-HOMING"])
p.req("R-FEED-HOLD", "Feed hold", "The firmware shall decelerate to rest inside its acceleration limits on a feed hold, and shall resume the same block without losing a step.", priority="should", refines=["CTRL.R-HOLD"])
p.req("R-STATUS-RATE", "Status reports", "The firmware shall send a status report every 100 ms or sooner while connected.", refines=["CTRL.R-OPERATOR-VIEW"])
p.req("R-START-ALARM", "Starts in alarm", "The firmware shall start in the alarm state after every reset and shall move nothing until the operator clears it.", refines=["CTRL.R-NO-RESTART"])
p.req("R-EVENTS", "Events written", "The firmware shall write every alarm, reset, homing and program start and end to the event store with its time.", priority="could", refines=["CTRL.R-RECORD"])
p.heading("H-APP", "Application requirements")
p.req("R-PLATFORMS", "Where it runs", "The application shall run on Windows 10 and later, macOS 13 and later, and Ubuntu 22.04 and later.", refines=["CTRL.R-PROGRAM"])
p.req("R-LOAD", "Loading a program", "The application shall load a G-code file of up to 50 MB and show its toolpath within 5 s.", refines=["CTRL.R-PROGRAM"])
p.req("R-CHECK", "Check before start", "The application shall refuse to start a program that would leave the travel of the machine or that uses a word the firmware does not accept, and shall name the first line at fault.", refines=["CTRL.R-PROGRAM", "CTRL.R-TRAVEL-GUARD"])
p.req("R-DISPLAY", "Live display", "The application shall show tool position, spindle speed, feed rate and machine state, updated five times a second or more.", "demonstration", refines=["CTRL.R-OPERATOR-VIEW"])
p.req("R-ALARM-TEXT", "Alarms in words", "The application shall show each alarm with what caused it and what the operator does next.", "inspection", refines=["CTRL.R-OPERATOR-VIEW"])
p.req("R-RESUME-UI", "Resume from a line", "The application shall let the operator resume a stopped program from the interrupted line and shall restore the spindle speed, coolant and offsets before the tool moves.", priority="should", refines=["CTRL.R-HOLD"])
p.req("R-TOOL-TABLE", "Tool length probing", "The application shall run a tool length measurement and store the result in the tool table without the operator typing a number.", "demonstration", "should", refines=["CTRL.R-TOOL-LENGTH"])
p.req("R-EXPORT-LOG", "Export the record", "The application shall export the running hours and events as a CSV file.", "demonstration", "could", refines=["CTRL.R-RECORD"])
p.req("R-START-GUARD", "Cycle start", "The application shall start a cycle only from a dedicated control that the operator holds for one second.", "demonstration", refines=["CTRL.R-NO-RESTART"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-WRONG-OFFSET", "Start with the wrong work offset", "The operator starts a program with the offset of the last job. Harm: the tool drives into the vice; broken tool, ejected fragment.", "medium")
p.heading("H-DES", "Design")
p.design("D-PLANNER", "Planner and step generator", "A ring of planned blocks feeds a timer interrupt that makes the step pulses. Corner speed comes from a junction deviation setting.", satisfies=["R-PLAN", "R-JITTER", "R-FEED-HOLD"])
p.design("D-STATES", "State machine", "Alarm, Idle, Home, Run, Hold and Probe. Every reset enters Alarm; only an operator command leaves it.", satisfies=["R-START-ALARM", "R-HOME-SEQ", "R-SOFT-LIMIT", "R-HARD-LIMIT"])
p.design("D-PARSER", "Line parser with a word table", "One table lists every accepted word and its modal group; a word that is not in the table is an error. The application reads the same table for its check before start.", satisfies=["R-PARSE", "R-CHECK"])
p.design("D-PROTOCOL", "Line protocol over TCP", "Text commands in, status lines out, on one TCP connection. The status line carries state, position, feed, speed and alarm code.", satisfies=["R-STATUS-RATE", "R-EVENTS", "R-EXPORT-LOG"])
p.design("D-APP", "Desktop application with a machine service", "One application for all three platforms. A background service owns the connection to the controller; the window can close without dropping a running job.", satisfies=["R-PLATFORMS", "R-DISPLAY", "R-LOAD"])
p.design("D-PREFLIGHT", "Pre-flight panel", "Before a start the application shows the program's extent against the travel, the active offset and the fitted tool, and the operator confirms each.", satisfies=["R-CHECK", "R-START-GUARD", "R-RESUME-UI", "R-TOOL-TABLE"], mitigates=["HZ-WRONG-OFFSET"])
p.design("D-ALARMS", "Alarm catalogue", "One file lists every alarm code with its cause and recovery; the application and the manual both read it.", satisfies=["R-ALARM-TEXT"])
p.heading("H-TEST", "Tests")
p.test("T-CONFORM", "G-code conformance suite", "Run 400 programs through the parser on the host and compare the planned moves with the reference. Pass: every accepted program matches and every rejected one names its line.", verifies=["R-PARSE"], execution="automated")
p.test("T-PLANNER", "Planner unit tests", "Unit tests for corner speed, look-ahead depth and soft limits on the host build. Pass: all pass.", verifies=["R-PLAN", "R-SOFT-LIMIT"], execution="automated")
p.test("T-JITTER", "Step jitter and limit response", "Capture step outputs at 250 kHz for 60 s against the plan, then drive a limit input and time the last step. Pass: every edge within 2 µs, last step within 100 µs.", verifies=["R-JITTER", "R-HARD-LIMIT"])
p.test("T-HIL", "Hardware-in-the-loop run", "On the test rig with simulated axes: home, run a program with 20 feed holds, and reset mid-program; record the status stream and read back the events. Pass: homing order as specified, no lost steps, alarm after every reset, no status gap over 100 ms, every event present.", verifies=["R-HOME-SEQ", "R-FEED-HOLD", "R-START-ALARM", "R-STATUS-RATE", "R-EVENTS"], execution="automated")
p.test("T-INSTALL", "Install matrix", "Install and connect to the simulator on Windows 10, Windows 11, macOS 13, macOS 15, Ubuntu 22.04 and Ubuntu 24.04. Pass: installs, connects and runs the sample program on all six.", verifies=["R-PLATFORMS"], execution="automated")
p.test("T-LOAD", "Large program", "Load a 50 MB program on the reference laptop ten times. Pass: toolpath shown within 5 s each time.", verifies=["R-LOAD"], execution="automated")
p.test("T-PREFLIGHT", "Pre-flight checks", "Try to start programs that leave the travel on each axis and programs with unknown words; try a short press on cycle start. Pass: every one refused with its line named, and the short press does nothing.", verifies=["R-CHECK", "R-START-GUARD"], execution="automated")
p.test("T-SIM", "Operation against the simulator", "Run a program, hold, resume from a line, probe a tool and export the record. Pass: every step does what its requirement says, and the display updates five times a second.", verifies=["R-DISPLAY", "R-RESUME-UI", "R-TOOL-TABLE", "R-EXPORT-LOG"], execution="mixed")
p.test("T-ALARM-REVIEW", "Alarm text review", "Raise every alarm on the simulator and read its text against the catalogue. Pass: each has a cause and a next step, and none shows only a code.", verifies=["R-ALARM-TEXT"])


# --------------------------------------------------------------------------
# Enclosure
# --------------------------------------------------------------------------
p = Project(
    "ENC",
    "Mill enclosure",
    "SYS",
    "The shell around the work zone, its door and window, the chip tray, the coolant containment and the lighting.",
    owner="Mechanical team",
)
p.heading("H-REQ", "Requirements")
p.req("R-ENCLOSE", "Work zone enclosed", "The enclosure shall close the work zone on every side with no opening wider than 6 mm while the door is shut.", "inspection", refines=["SYS.R-CONTAIN"])
p.req("R-IMPACT", "Window impact", "The window shall stop a 20 g steel projectile at 40 m/s without the projectile passing through.", refines=["SYS.R-CONTAIN"])
p.req("R-DOOR-TRAVEL", "Door and its switch", "The door shall carry the interlock actuator so that the switch operates before the door has opened 10 mm, at every point along its edge.", refines=["SYS.R-DOOR"])
p.req("R-CHIPS", "Chip tray", "The enclosure shall hold 3 litres of chips in a tray the operator can remove without tools.", "demonstration", "should", refines=["SYS.R-CONTAIN"])
p.req("R-COOLANT", "Coolant", "The enclosure shall leak no coolant during 30 minutes of flood cooling at 2 litres a minute.", refines=["SYS.R-CONTAIN"])
p.req("R-ATTENUATE", "Noise reduction", "The closed enclosure shall reduce the sound level at 1 m by 10 dB or more.", refines=["SYS.R-NOISE"])
p.req("R-ENC-MASS", "Enclosure mass", "The enclosure shall weigh no more than 14 kg.", "inspection", refines=["SYS.R-MASS"])
p.req("R-ENC-SIZE", "Outside dimensions", "The enclosure shall measure no more than 700 mm wide, 600 mm deep and 800 mm high with the door shut.", "inspection", refines=["SYS.R-SIZE"])
p.req("R-LIGHT", "Lighting", "The enclosure shall light the table to 500 lux or more.", priority="could", refines=["SYS.R-STATUS"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-WINDOW-AGE", "Window weakened by coolant", "Coolant makes the polycarbonate brittle over years, so it no longer stops a fragment. Harm: eye injury.", "medium")
p.heading("H-DES", "Design")
p.design("D-SHELL", "Sheet-steel shell with a lined interior", "A 1.2 mm steel shell lined with damping sheet, with labyrinth seals at the door and a sloped floor to the chip tray.", satisfies=["R-ENCLOSE", "R-ATTENUATE", "R-COOLANT", "R-CHIPS", "R-ENC-MASS", "R-ENC-SIZE"])
p.design("D-WINDOW", "Laminated window with a replace-by date", "A 6 mm polycarbonate window with a hard-coated inner sheet that takes the coolant, marked with the date it must be replaced.", satisfies=["R-IMPACT"], mitigates=["HZ-WINDOW-AGE"])
p.design("D-DOOR", "Sliding door on a rigid frame", "A door that slides on two rails in a welded frame, stiff enough that the actuator edge cannot be sprung open past the switch point.", satisfies=["R-DOOR-TRAVEL"])
p.design("D-LIGHT", "Sealed light strip", "A sealed LED strip under the top panel, out of the chip path.", satisfies=["R-LIGHT"])
p.heading("H-TEST", "Tests")
p.test("T-IMPACT", "Window impact test", "Fire a 20 g steel projectile at 40 m/s at the centre and at a corner of three windows, one aged in coolant for 1,000 hours. Pass: no projectile passes through.", verifies=["R-IMPACT"])
p.test("T-DOOR-TRAVEL", "Door opening at the switch point", "Pull the door at five points along its edge with 100 N and measure the gap when the switch operates. Pass: under 10 mm at every point.", verifies=["R-DOOR-TRAVEL"])
p.test("T-FLOOD", "Flood and chip test", "Run flood coolant at 2 litres a minute for 30 minutes while cutting; then remove the chip tray. Pass: no coolant outside, tray holds 3 litres, tray out without tools.", verifies=["R-COOLANT", "R-CHIPS"])
p.test("T-ATTENUATE", "Noise reduction measurement", "Measure sound level at 1 m with a reference source inside, door open and door shut. Pass: 10 dB or more lower with the door shut.", verifies=["R-ATTENUATE"])
p.test("T-ENC-INSPECT", "Enclosure inspection", "Probe every gap with a 6 mm rod, weigh and measure the enclosure, and measure light at the table. Pass: every figure within its requirement.", verifies=["R-ENCLOSE", "R-ENC-MASS", "R-ENC-SIZE", "R-LIGHT"])


# --------------------------------------------------------------------------
# Safety
# --------------------------------------------------------------------------
p = Project(
    "SAFE",
    "Mill safety",
    "SYS",
    "The safety system of the Desktop CNC Mill: the emergency stop, the door interlock, the safety relay that "
    "turns either into a stop demand, the reset, and the protective earth.",
    owner="Safety engineer",
)
p.heading("H-REQ", "Requirements")
p.req("R-ESTOP-BUTTON", "Emergency stop button", "The mill shall carry a red emergency stop button on a yellow ground that latches when pressed and that the operator can reach from the working position without opening the door.", "inspection", refines=["SYS.R-ESTOP"])
p.req("R-DEMAND", "Stop demand", "The safety system shall demand a stop from the control within 10 ms of either channel of the emergency stop or of the door interlock opening.", refines=["SYS.R-ESTOP", "SYS.R-DOOR"])
p.req("R-SINGLE-FAULT", "A single fault does not lose the stop", "The safety system shall still demand a stop when any one of its components has failed, and shall refuse to reset until the fault is cleared.", "analysis", refines=["SYS.R-ESTOP", "SYS.R-DOOR"])
p.req("R-INTERLOCK", "Interlock switch", "The door interlock shall be a coded switch with two channels that both open before the door has opened 10 mm.", refines=["SYS.R-DOOR"])
p.req("R-NO-DEFEAT", "Interlock cannot be defeated simply", "The door interlock shall not be operated by a plain magnet, tape, or any object an operator would have at the machine.", "demonstration", refines=["SYS.R-DOOR"])
p.req("R-RESET", "Manual reset", "The safety system shall withdraw a stop demand only when the operator presses reset with the emergency stop released and the door shut, and never on its own when power returns.", refines=["SYS.R-RESTART"])
p.req("R-EARTH", "Protective earth", "Every exposed metal part of the mill shall be bonded to the protective earth terminal with a resistance of 0.1 Ω or less.", refines=["SYS.R-SUPPLY"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-WELDED", "A welded relay contact", "A relay contact in the safety chain welds shut, so the next stop does nothing. Harm: the machine keeps moving after an emergency stop.", "high")
p.hazard("HZ-DEFEAT", "Interlock defeated", "The operator holds the interlock closed to watch the cut with the door open. Harm: contact with the tool; eye injury.", "high")
p.heading("H-DES", "Design")
p.design("D-RELAY", "Safety relay with monitored contacts", "A safety relay with force-guided contacts on two channels. It checks its own contacts before each reset, and its output is the stop input of the control electronics.", satisfies=["R-DEMAND", "R-SINGLE-FAULT", "R-RESET"], mitigates=["HZ-WELDED"])
p.design("D-SWITCH", "Coded interlock", "A coded non-contact switch whose actuator is riveted to the door; a plain magnet does not operate it.", satisfies=["R-INTERLOCK", "R-NO-DEFEAT"], mitigates=["HZ-DEFEAT"])
p.design("D-STATION", "Operator station", "The emergency stop and the reset button on the front right of the enclosure, 150 mm above the bench.", satisfies=["R-ESTOP-BUTTON"])
p.design("D-BONDING", "Star earth", "Every panel, the frame and the cabinet are bonded by their own conductor to one earth stud at the mains inlet.", satisfies=["R-EARTH"])
p.heading("H-TEST", "Tests")
p.test("T-DEMAND", "Stop demand timing", "Open each channel of the emergency stop and of the interlock in turn, 20 times each, and time the relay output. Pass: within 10 ms every time.", verifies=["R-DEMAND"])
p.test("T-FAULT-INSERT", "Fault insertion and failure analysis", "Short and open each safety input in turn and hold one relay contact closed; review the result against the failure modes and effects analysis. Pass: a stop is still demanded and reset is refused in every case.", verifies=["R-SINGLE-FAULT"])
p.test("T-INTERLOCK", "Interlock function and defeat", "Open the door slowly and record when each channel opens; try to operate the switch with a plain magnet, tape and a spare door handle. Pass: both channels open before 10 mm and no defeat works.", verifies=["R-INTERLOCK", "R-NO-DEFEAT"])
p.test("T-RESET", "Reset conditions", "Try reset with the emergency stop pressed, with the door open, and after a power cut with nothing pressed. Pass: the demand is withdrawn only with both clear and the button pressed.", verifies=["R-RESET"])
p.test("T-SAFE-INSPECT", "Button and earth bond", "Check the emergency stop for colour, latching and reach, and measure the bond from the earth stud to every exposed metal part at 10 A. Pass: reachable with the door shut, and 0.1 Ω or less everywhere.", verifies=["R-ESTOP-BUTTON", "R-EARTH"])


# --------------------------------------------------------------------------
# The delivery schedule: January 2026 to December 2027
# --------------------------------------------------------------------------
# Three tiers. Requirements flow down, so the mill defines first and the
# parts last; deliveries flow up, so a part is reviewed, built, verified and
# released before the assembly it goes into, and that before the mill.
#
# Each project has five phases, each a summary task with its tasks under it
# and a milestone at its end. A task is (key, title, start, end); a
# milestone is (name, date, criteria). load.py works out progress from the
# day it runs: a task that has ended is done, one under way is part done.

SCHEDULE = {
    "system": [
        ("Define", [
            ("A", "Stakeholder needs and personas", "2026-01-05", "2026-02-06"),
            ("B", "System requirements", "2026-02-02", "2026-03-20"),
            ("C", "System requirements review", "2026-03-23", "2026-03-27"),
        ], ("System requirements review", "2026-03-27", [])),
        ("Preliminary design", [
            ("D", "Architecture and budgets", "2026-03-30", "2026-05-29"),
            ("E", "Hazard analysis", "2026-04-13", "2026-07-03"),
            ("F", "Preliminary design review", "2026-07-13", "2026-07-17"),
        ], ("Preliminary design review", "2026-07-17", [])),
        ("Detailed design", [
            ("G", "System design and interface control", "2026-07-20", "2027-01-15"),
            ("H", "Verification plan and test cases", "2026-09-14", "2027-01-22"),
            ("I", "Critical design review", "2027-01-25", "2027-01-29"),
        ], ("Critical design review", "2027-01-29", [("requirements_designed", 100)])),
        ("Alpha machine", [
            ("J", "Order long-lead parts", "2026-11-02", "2027-02-26"),
            ("K", "Assemble the alpha machine", "2027-04-26", "2027-05-07"),
            ("L", "Commission and first cuts", "2027-05-10", "2027-05-28"),
        ], ("Alpha machine cutting", "2027-05-28", [])),
        ("Verification", [
            ("M", "System verification tests", "2027-06-01", "2027-08-27"),
            ("N", "First-part and classroom validation", "2027-08-02", "2027-09-17"),
            ("O", "Fix and retest", "2027-08-16", "2027-09-24"),
        ], ("Verification complete", "2027-09-24", [("requirements_verified", 100), ("tests_passing", 100)])),
        ("Release", [
            ("P", "Pilot build of ten machines", "2027-09-27", "2027-11-12"),
            ("Q", "Manual and service documents", "2027-09-06", "2027-11-19"),
            ("R", "Production release review", "2027-11-22", "2027-12-03"),
        ], None),  # the release's own milestone
    ],
    "assembly": [
        ("Define", [
            ("A", "Requirements from the mill", "2026-03-02", "2026-04-10"),
            ("B", "Allocation to its parts", "2026-03-30", "2026-04-24"),
        ], ("Requirements agreed", "2026-04-24", [])),
        ("Preliminary design", [
            ("D", "Layout and budgets", "2026-04-27", "2026-06-19"),
            ("F", "Preliminary design review", "2026-06-29", "2026-07-03"),
        ], ("Preliminary design review", "2026-07-03", [])),
        ("Detailed design", [
            ("G", "Integration design", "2026-07-06", "2026-12-31"),
            ("H", "Interface control and analysis", "2026-09-21", "2027-01-08"),
            ("I", "Critical design review", "2027-01-11", "2027-01-15"),
        ], ("Critical design review", "2027-01-15", [("requirements_designed", 100)])),
        ("Prototype", [
            ("J", "Integration rig", "2026-11-16", "2027-03-12"),
            ("K", "Integrate the delivered parts", "2027-03-29", "2027-04-16"),
            ("L", "Checks and hand over to machine assembly", "2027-04-19", "2027-04-23"),
        ], ("Delivered to machine assembly", "2027-04-23", [])),
        ("Verification", [
            ("M", "Integration tests", "2027-05-31", "2027-07-30"),
            ("O", "Fix and retest", "2027-07-19", "2027-08-20"),
        ], ("Verified", "2027-08-20", [("requirements_verified", 100)])),
        ("Release", [
            ("Q", "Production documents", "2027-08-23", "2027-11-05"),
            ("R", "Production release review", "2027-11-08", "2027-11-19"),
        ], ("Released for production", "2027-11-19", [])),
    ],
    "part": [
        ("Define", [
            ("A", "Requirements from the assembly", "2026-03-30", "2026-05-08"),
            ("B", "Interfaces agreed", "2026-04-27", "2026-05-22"),
        ], ("Requirements agreed", "2026-05-22", [])),
        ("Preliminary design", [
            ("D", "Concept and layout", "2026-05-11", "2026-06-12"),
            ("F", "Preliminary design review", "2026-06-15", "2026-06-19"),
        ], ("Preliminary design review", "2026-06-19", [])),
        ("Detailed design", [
            ("G", "Detailed design and drawings", "2026-06-22", "2026-12-04"),
            ("H", "Analysis and calculations", "2026-09-07", "2026-12-11"),
            ("I", "Critical design review", "2026-12-14", "2026-12-18"),
        ], ("Critical design review", "2026-12-18", [("requirements_designed", 100)])),
        ("Prototype", [
            ("J", "Order materials and parts", "2026-11-02", "2027-01-29"),
            ("K", "Make and assemble the prototype", "2027-01-18", "2027-03-19"),
            ("L", "Inspect and hand over", "2027-03-22", "2027-03-26"),
        ], ("Prototype delivered", "2027-03-26", [])),
        ("Verification", [
            ("M", "Verification tests", "2027-04-05", "2027-06-25"),
            ("O", "Fix and retest", "2027-06-14", "2027-07-16"),
        ], ("Verified", "2027-07-16", [("requirements_verified", 100), ("tests_passing", 100)])),
        ("Release", [
            ("Q", "Production drawings and suppliers", "2027-07-19", "2027-10-15"),
            ("R", "Production release review", "2027-10-18", "2027-10-29"),
        ], ("Released for production", "2027-10-29", [])),
    ],
}

# Finish-to-start within a project: the second starts after the first ends.
FOLLOWS = [("B", "C"), ("D", "F"), ("G", "I"), ("H", "I"), ("K", "L"), ("L", "M"), ("Q", "R")]

# The top-level release. Its milestone on the mill's plan takes the Release
# phase's tasks, and every requirement of the mill targets it.
RELEASE = {
    "project": "SYS",
    "name": "Desktop CNC Mill 1.0",
    "description": "The first production release: ten pilot machines built, the manual printed, and every requirement verified.",
    "target_date": "2027-12-10",
}

# code -> (tier, how far ahead (+) or behind (-) its work in progress is, as
# a fraction of the task, and titles that differ from the tier's)
PLANS = {
    "SYS": ("system", -0.05, {}),
    "FRAME": ("assembly", 0.05, {}),
    "MOTION": ("assembly", -0.10, {"J": "Axis test rig", "M": "Motion tests on the rig and the machine"}),
    "CTRL": ("assembly", -0.15, {"J": "Simulator and bench controller", "K": "Integrate electronics and software", "M": "Control tests on the bench and the machine"}),
    "ENC": ("assembly", 0.10, {"A": "Requirements from the mill", "B": "Interfaces to the frame and the safety system", "D": "Concept and mock-up", "G": "Sheet-metal design and drawings", "H": "Acoustic and impact analysis", "J": "Order sheet metal and windows", "K": "Make and fit the prototype enclosure", "M": "Impact, flood and noise tests", "Q": "Production drawings and tooling"}),
    "SAFE": ("assembly", -0.20, {"A": "Requirements from the mill", "B": "Risk assessment", "D": "Safety concept", "G": "Safety circuit design", "H": "Failure modes and effects analysis", "J": "Order safety components", "K": "Build and wire the safety system", "M": "Fault insertion and timing tests", "Q": "Safety file and declarations"}),
    "BASE": ("part", 0.10, {"J": "Order the casting pattern and steel", "K": "Cast, weld and grind the prototype"}),
    "XY": ("part", 0.0, {}),
    "ZAX": ("part", -0.10, {}),
    "XDRV": ("part", 0.05, {"D": "Motor and screw sizing", "J": "Order motors, drives and screws"}),
    "YDRV": ("part", 0.0, {"D": "Motor and screw sizing", "J": "Order motors, drives and screws"}),
    "ZDRV": ("part", -0.15, {"D": "Motor, screw and brake sizing", "J": "Order motors, drives, screws and brake"}),
    "SPIN": ("part", -0.25, {"A": "Specification sent to suppliers", "B": "Supplier chosen and contract signed", "D": "Supplier's proposal", "G": "Supplier's detailed design", "H": "Drive integration analysis", "J": "First articles ordered", "K": "First articles built by the supplier", "L": "Incoming inspection and hand over", "M": "Acceptance tests at the supplier and in house", "Q": "Production approval of the supplier"}),
    "ELEC": ("part", 0.0, {"D": "Block diagram and part selection", "G": "Schematic and board layout", "H": "Timing and thermal analysis", "J": "Order boards and parts", "K": "Assemble and bring up the prototype boards"}),
    "SW": ("part", -0.10, {"D": "Architecture and simulator", "G": "Firmware and application for alpha", "H": "Automated test suites", "J": "Hardware-in-the-loop bench", "K": "Alpha build on the prototype board", "L": "Alpha handed over", "M": "Conformance, timing and usability tests", "Q": "Release candidate and installers"}),
}

# A parent task that waits for its children's deliveries: the parent's task
# K starts after each child's hand-over, task L.
WAITS_FOR_CHILDREN = {"assembly": "K", "system": "K"}
