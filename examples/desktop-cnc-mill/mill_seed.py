"""Draft content for the Desktop CNC Mill sample programme.

Seven projects in a tree. Each child's requirements refine requirements of
its parent, so verification rolls up from the sub-projects to the mill.
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
    "drawing tolerance. The top of the programme: its requirements flow down to the frame, spindle, controller, "
    "software and enclosure projects, and their verification rolls back up here.",
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
p.design("D-ARCH", "Three-axis moving-table layout", "A fixed column carries the Z axis and spindle; the table moves in X and Y on a cast base. The frame, the spindle, the controller, the control software and the enclosure are each a project under this one.", satisfies=["R-ENVELOPE", "R-SIZE", "R-MASS"])
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
# Frame and axes
# --------------------------------------------------------------------------
p = Project(
    "FRAME",
    "Mill Frame and Axes",
    "SYS",
    "The base, column, table, linear rails, ballscrews and axis motors of the Desktop CNC Mill. Owns travel, "
    "accuracy, stiffness and most of the mass.",
    owner="Mechanical team",
)
p.heading("H-REQ", "Requirements")
p.req("R-TRAVEL", "Axis travel", "The frame shall give 305 mm of travel in X, 185 mm in Y and 205 mm in Z between limit switches.", "inspection", refines=["SYS.R-ENVELOPE"])
p.req("R-STRAIGHT", "Straightness", "Each axis shall move straight within 0.008 mm over any 100 mm of travel.", refines=["SYS.R-ACCURACY"])
p.req("R-SQUARE", "Squareness", "Each pair of axes shall be square within 0.012 mm over 100 mm.", refines=["SYS.R-ACCURACY"])
p.req("R-BACKLASH", "Backlash", "Each axis shall have 0.005 mm of backlash or less at the table.", refines=["SYS.R-REPEAT", "SYS.R-ACCURACY"])
p.req("R-STIFF", "Stiffness at the tool", "The frame shall deflect no more than 0.01 mm at the tool point under a 150 N load in X, Y or Z.", refines=["SYS.R-REMOVAL-AL", "SYS.R-REMOVAL-ST", "SYS.R-FINISH"])
p.req("R-RESONANCE", "First resonance", "The assembled frame shall have its first natural frequency at 120 Hz or above.", "analysis", "should", refines=["SYS.R-FINISH"])
p.req("R-AXIS-SPEED", "Axis speed under load", "Each axis shall reach 5.5 m/min carrying a 20 kg load on the table.", refines=["SYS.R-RAPID"])
p.req("R-FRAME-MASS", "Frame mass", "The frame with its axes and motors shall weigh no more than 62 kg.", "inspection", refines=["SYS.R-MASS"])
p.req("R-LIMIT-MOUNT", "Limit switches at both ends", "Each axis shall carry a limit switch at each end of travel that operates 3 mm or more before the mechanical stop.", "inspection", refines=["SYS.R-LIMITS"])
p.req("R-LUBE-POINTS", "Lubrication", "Every rail carriage and ballscrew nut shall take grease through a nipple the operator can reach without removing a cover, and shall run 40 hours between greasings.", "demonstration", "should", refines=["SYS.R-LUBE"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-PINCH", "Pinch at the way covers", "A finger is caught where a way cover folds as the table reaches the end of travel. Harm: bruising or a cut.", "low")
p.heading("H-DES", "Design")
p.design("D-BASE", "Polymer-concrete base, steel column", "A cast polymer-concrete base damps vibration; a stress-relieved steel column bolts to it on scraped pads.", satisfies=["R-STIFF", "R-RESONANCE", "R-FRAME-MASS"])
p.design("D-RAILS", "Preloaded profile rails", "Size 15 profile rails with two preloaded carriages per rail on each axis.", satisfies=["R-STRAIGHT", "R-SQUARE", "R-STIFF", "R-LUBE-POINTS"])
p.design("D-SCREWS", "Ground ballscrews with double nuts", "12 mm ground ballscrews, 4 mm lead, with preloaded double nuts and fixed-floating bearing supports, driven by closed-loop stepper motors.", satisfies=["R-BACKLASH", "R-AXIS-SPEED", "R-TRAVEL"])
p.design("D-COVERS", "Way covers with finger clearance", "Bellows covers that keep 25 mm clear at full compression.", mitigates=["HZ-PINCH"])
p.heading("H-TEST", "Tests")
p.test("T-GEOMETRY", "Geometry on the surface plate", "Check straightness of each axis against a straightedge and squareness against a granite square with a 0.001 mm indicator. Pass: straightness within 0.008 mm and squareness within 0.012 mm per 100 mm.", verifies=["R-STRAIGHT", "R-SQUARE"])
p.test("T-BACKLASH", "Backlash by reversal", "Approach a point from both directions on each axis ten times with an indicator on the table. Pass: mean difference 0.005 mm or less.", verifies=["R-BACKLASH"])
p.test("T-STIFF", "Static stiffness", "Load the tool point to 150 N in each direction through a load cell and read deflection. Pass: 0.01 mm or less.", verifies=["R-STIFF"])
p.test("T-TRAVEL", "Travel, limits and mass", "Measure travel between limit switches and the gap from switch point to hard stop on each axis; weigh the frame. Pass: every figure within its requirement.", verifies=["R-TRAVEL", "R-LIMIT-MOUNT", "R-FRAME-MASS"])
p.test("T-SPEED", "Axis speed run", "Run each axis at 5.5 m/min over full travel with 20 kg on the table, 50 cycles. Pass: no following error alarm and no lost position.", verifies=["R-AXIS-SPEED"])
p.test("T-MODAL", "Modal analysis and tap test", "Find the first natural frequency of the assembled frame by finite-element analysis and confirm it with an accelerometer and an instrumented hammer. Pass: 120 Hz or above by both.", verifies=["R-RESONANCE"])
p.test("T-GREASE", "Greasing by an operator", "After a 40-hour run, an operator greases every point from the manual with the supplied gun. Pass: no cover removed, every point reached, and no rail or screw ran dry.", verifies=["R-LUBE-POINTS"])


# --------------------------------------------------------------------------
# Spindle and drive (supplier)
# --------------------------------------------------------------------------
p = Project(
    "SPIN",
    "Mill Spindle and Drive",
    "SYS",
    "The spindle cartridge and its drive, built by a supplier to the mill's requirements. The supplier works in "
    "this project and sees the mill's requirements it refines, and nothing else of the programme.",
    owner="Northfield Spindles (supplier)",
)
p.heading("H-REQ", "Requirements")
p.req("R-SPEED", "Speed range", "The spindle shall run at any speed from 2,000 to 24,000 rpm, held within 2 % of the commanded speed.", refines=["SYS.R-FINISH", "SYS.R-REMOVAL-AL"])
p.req("R-POWER", "Continuous power", "The spindle shall deliver 1.1 kW or more continuously at 12,000 rpm.", refines=["SYS.R-REMOVAL-AL", "SYS.R-REMOVAL-ST"])
p.req("R-RUNOUT", "Runout", "The spindle taper shall run out no more than 0.005 mm.", refines=["SYS.R-FINISH", "SYS.R-ACCURACY"])
p.req("R-COLLET", "Tool holding", "The spindle shall hold tools with shanks from 1 mm to 13 mm in ER20 collets.", "inspection", refines=["SYS.R-REMOVAL-AL"])
p.req("R-STOP", "Stopping time", "The drive shall bring the spindle from 24,000 rpm to rest within 2.5 s of its safe-torque-off input opening.", refines=["SYS.R-ESTOP", "SYS.R-DOOR"])
p.req("R-STO", "Safe torque off", "The drive shall remove torque from the motor when either of its two safe-torque-off channels opens, and shall report a disagreement between the channels.", refines=["SYS.R-ESTOP"])
p.req("R-CURRENT", "Supply current", "The drive shall draw no more than 6 A from a 230 V supply at full load.", refines=["SYS.R-SUPPLY"])
p.req("R-SPIN-NOISE", "Noise", "The spindle shall emit no more than 68 dB(A) at 1 m running free at 24,000 rpm.", refines=["SYS.R-NOISE"])
p.req("R-THERMAL", "Temperature rise", "The spindle nose shall rise no more than 30 K above ambient after one hour at 18,000 rpm.", priority="should", refines=["SYS.R-ACCURACY"])
p.req("R-CARTRIDGE", "Cartridge replacement", "One person shall be able to replace the spindle cartridge within 20 minutes by removing four screws and one connector.", "demonstration", "should", refines=["SYS.R-SPINDLE-SWAP"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-OVERSPEED", "Overspeed", "A drive fault runs the spindle above the rated speed of the tool. Harm: tool bursts.", "high")
p.hazard("HZ-HOT", "Hot spindle nose", "The operator touches the spindle nose when changing a tool after a long run. Harm: minor burn.", "low")
p.heading("H-DES", "Design")
p.design("D-CARTRIDGE", "Brushless cartridge spindle", "A brushless motor on the spindle shaft, running in two pairs of angular-contact bearings greased for life, in a cartridge that bolts into the head.", satisfies=["R-SPEED", "R-POWER", "R-RUNOUT", "R-COLLET", "R-CARTRIDGE", "R-SPIN-NOISE"])
p.design("D-DRIVE", "Drive with safe torque off and a brake resistor", "A sensorless vector drive with a two-channel safe-torque-off input, a brake resistor for fast stops, and a speed limit set in hardware.", satisfies=["R-STOP", "R-STO", "R-CURRENT"], mitigates=["HZ-OVERSPEED"])
p.design("D-COOLING", "Shaft-driven fan and a finned housing", "A fan on the shaft draws air over a finned housing; a thermistor at the front bearing reports temperature.", satisfies=["R-THERMAL"], mitigates=["HZ-HOT"])
p.heading("H-TEST", "Tests")
p.test("T-DYNO", "Power and current on the dynamometer", "Load the spindle to 1.1 kW at 12,000 rpm for 30 minutes and record speed, shaft power and supply current. Pass: power held, speed within 2 %, current 6 A or less.", verifies=["R-POWER", "R-SPEED", "R-CURRENT"])
p.test("T-RUNOUT", "Runout with a test bar", "Indicate a ground test bar in the taper at the nose and 50 mm out, at four collet positions. Pass: 0.005 mm or less at the nose.", verifies=["R-RUNOUT", "R-COLLET"])
p.test("T-STOP", "Stop on safe torque off", "Open each safe-torque-off channel in turn, then both, at 24,000 rpm. Pass: at rest within 2.5 s each time, and a single open channel is reported.", verifies=["R-STOP", "R-STO"])
p.test("T-SOAK", "Thermal soak and noise", "Run at 18,000 rpm for one hour, logging nose temperature; then measure sound level at 24,000 rpm. Pass: rise 30 K or less and 68 dB(A) or less.", verifies=["R-THERMAL", "R-SPIN-NOISE"])
p.test("T-SWAP", "Cartridge swap", "A technician who has not done it before replaces the cartridge from the service sheet. Pass: 20 minutes or less, and runout within limit afterwards.", verifies=["R-CARTRIDGE"])


# --------------------------------------------------------------------------
# Controller
# --------------------------------------------------------------------------
p = Project(
    "CTRL",
    "Mill Controller",
    "SYS",
    "The electronics of the Desktop CNC Mill: the controller board, the safety relay, the supplies and the wiring. "
    "The motion firmware that runs on the board is a project under this one.",
    owner="Electronics team",
)
p.heading("H-REQ", "Requirements")
p.req("R-ESTOP-CIRCUIT", "Emergency stop circuit", "The controller shall remove the enable from every axis drive and open the spindle drive's safe-torque-off input within 20 ms of either emergency stop channel opening.", refines=["SYS.R-ESTOP"])
p.req("R-SINGLE-FAULT", "A single fault does not lose the stop", "The controller shall still stop the machine when any one component of the emergency stop or door circuit has failed, and shall refuse to reset until the fault is cleared.", "analysis", refines=["SYS.R-ESTOP", "SYS.R-DOOR"])
p.req("R-DOOR-INPUT", "Door input", "The controller shall treat the door as open when either of its two interlock channels is open.", refines=["SYS.R-DOOR"])
p.req("R-NO-RESTART", "Reset after power returns", "The controller shall keep the drives disabled after power returns until the operator presses reset.", refines=["SYS.R-RESTART"])
p.req("R-BRAKE", "Z brake", "The controller shall release the Z brake only while the drives are enabled, and the brake shall hold within 50 ms of power being removed.", refines=["SYS.R-ZHOLD"])
p.req("R-MOTION", "Coordinated motion", "The controller shall move the three axes together along a programmed path with a path error of 0.005 mm or less at feeds up to 2 m/min.", refines=["SYS.R-ACCURACY", "SYS.R-GCODE"])
p.req("R-HOMING", "Homing", "The controller shall home the three axes within 45 s.", refines=["SYS.R-HOME"])
p.req("R-LIMIT-INPUT", "Limit inputs", "The controller shall stop an axis within 1 mm of its limit switch operating.", refines=["SYS.R-LIMITS"])
p.req("R-REPORT", "State reports", "The controller shall send position, spindle speed, feed rate and alarm state to the control software ten times a second or more.", refines=["SYS.R-STATUS"])
p.req("R-HOLD", "Hold and resume", "The controller shall stop on a feed hold without losing position and shall resume from the same point in the program.", priority="should", refines=["SYS.R-RESUME"])
p.req("R-PROBE-INPUT", "Probe input", "The controller shall capture the axis positions within 0.002 mm of where the probe input changed.", priority="should", refines=["SYS.R-PROBE"])
p.req("R-EVENT-LOG", "Event log", "The controller shall keep the last 10,000 events and the running hours through a power cut.", priority="could", refines=["SYS.R-LOG"])
p.req("R-CTRL-CURRENT", "Supply current", "The controller and the axis drives together shall draw no more than 3.5 A from a 230 V supply.", refines=["SYS.R-SUPPLY"])
p.req("R-EARTH", "Protective earth", "Every exposed metal part of the mill shall be bonded to the protective earth terminal with a resistance of 0.1 Ω or less.", refines=["SYS.R-SUPPLY"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-WELDED", "A welded relay contact", "A relay contact in the safety chain welds shut, so the next stop command does nothing. Harm: the machine keeps moving after an emergency stop.", "high")
p.hazard("HZ-COOLANT", "Coolant in the electronics", "Coolant mist reaches the controller. Harm: electric shock or fire.", "medium")
p.heading("H-DES", "Design")
p.design("D-BOARD", "Controller board", "A microcontroller board with step and direction outputs for four axes, two inputs per axis for limit and home, a probe input, and Ethernet.", satisfies=["R-MOTION", "R-HOMING", "R-LIMIT-INPUT", "R-REPORT", "R-PROBE-INPUT", "R-HOLD"])
p.design("D-RELAY", "Safety relay with monitored contacts", "A safety relay with force-guided contacts on two channels. It checks its own contacts before each reset, and it, not the firmware, removes the drive enable.", satisfies=["R-ESTOP-CIRCUIT", "R-SINGLE-FAULT", "R-DOOR-INPUT", "R-NO-RESTART"], mitigates=["HZ-WELDED"])
p.design("D-BRAKE-DRIVER", "Brake driver fed from the drive enable", "The Z brake coil is powered from the same switched rail as the drive enable, so the brake applies whenever the drives are off.", satisfies=["R-BRAKE"])
p.design("D-CABINET", "Sealed cabinet with a 48 V supply", "A gasketed cabinet behind the column holds the board, relay, 48 V supply and drives. Mains wiring is segregated and every panel is bonded to earth.", satisfies=["R-CTRL-CURRENT", "R-EARTH"], mitigates=["HZ-COOLANT"])
p.design("D-FRAM", "Non-volatile event store", "A ferroelectric memory holds the event ring and the hour counter and needs no battery.", satisfies=["R-EVENT-LOG"])
p.heading("H-TEST", "Tests")
p.test("T-ESTOP-TIMING", "Emergency stop timing on the bench", "Open each emergency stop channel and record the drive enable and safe-torque-off lines on an oscilloscope, 20 times. Pass: both lines change within 20 ms every time.", verifies=["R-ESTOP-CIRCUIT"])
p.test("T-FAULT-INSERT", "Fault insertion", "Short and open each safety input in turn, and hold one relay contact closed. Pass: the stop still works and reset is refused in every case.", verifies=["R-SINGLE-FAULT", "R-DOOR-INPUT"])
p.test("T-POWER-CYCLE", "Power cycling", "Interrupt the supply 50 times in every machine state. Pass: drives stay disabled until reset, the brake holds within 50 ms, and the event log and hours survive.", verifies=["R-NO-RESTART", "R-BRAKE", "R-EVENT-LOG"])
p.test("T-CONTOUR", "Contouring", "Cut a 50 mm circle and a 50 mm diamond at 2 m/min with an indicator pen on a ballbar. Pass: path error 0.005 mm or less.", verifies=["R-MOTION"])
p.test("T-IO", "Homing, limits and probe", "Home from ten random positions; run each axis into its limit at rapid; touch a probe at five feeds. Pass: home within 45 s, stop within 1 mm of the switch, and probe capture within 0.002 mm.", verifies=["R-HOMING", "R-LIMIT-INPUT", "R-PROBE-INPUT"])
p.test("T-ELECTRICAL", "Earth bond and supply current", "Measure the bond from the earth terminal to every exposed metal part at 10 A, and the supply current with all three axes at rapid. Pass: 0.1 Ω or less and 3.5 A or less.", verifies=["R-EARTH", "R-CTRL-CURRENT"])
p.test("T-LINK", "Reports and hold", "Log the state reports for ten minutes during a program with five feed holds. Pass: ten reports a second or more, and position unchanged across each hold.", verifies=["R-REPORT", "R-HOLD"], execution="automated")


# --------------------------------------------------------------------------
# Motion firmware (under the controller)
# --------------------------------------------------------------------------
p = Project(
    "FW",
    "Mill Motion Firmware",
    "CTRL",
    "The firmware on the controller board: it reads G-code, plans the motion, makes the step pulses and reports "
    "state. Two levels below the mill, so its test results roll up through the controller.",
    owner="Firmware team",
)
p.heading("H-REQ", "Requirements")
p.req("R-PARSE", "G-code it accepts", "The firmware shall accept G0, G1, G2, G3, G17 to G19, G20, G21, G28, G43, G49, G53, G54 to G59, G80 to G83, G90, G91, and M3, M5, M8, M9 and M30, and shall reject any other word with an alarm that names the line.", refines=["CTRL.R-MOTION"])
p.req("R-PLAN", "Look-ahead", "The firmware shall plan 32 blocks or more ahead and shall keep the path within 0.003 mm of the programmed corners.", refines=["CTRL.R-MOTION"])
p.req("R-JITTER", "Step timing", "The firmware shall place each step pulse within 2 µs of its planned time at step rates up to 250 kHz.", refines=["CTRL.R-MOTION"])
p.req("R-SOFT-LIMIT", "Soft limits", "The firmware shall refuse a move that would leave the travel of an axis and shall raise an alarm before it moves.", refines=["CTRL.R-LIMIT-INPUT"])
p.req("R-HARD-LIMIT", "Limit switch response", "The firmware shall stop stepping an axis within 100 µs of its limit input changing.", refines=["CTRL.R-LIMIT-INPUT"])
p.req("R-HOME-SEQ", "Homing sequence", "The firmware shall home Z first, then X and Y together, approaching each switch twice, the second time at 100 mm/min.", "demonstration", refines=["CTRL.R-HOMING"])
p.req("R-FEED-HOLD", "Feed hold", "The firmware shall decelerate to rest inside its acceleration limits on a feed hold, and shall resume the same block without losing a step.", priority="should", refines=["CTRL.R-HOLD"])
p.req("R-PROBE-LATCH", "Probe latch", "The firmware shall latch the step counts of all axes within 20 µs of the probe input changing.", priority="should", refines=["CTRL.R-PROBE-INPUT"])
p.req("R-STATUS-RATE", "Status reports", "The firmware shall send a status report every 100 ms or sooner while connected.", refines=["CTRL.R-REPORT"])
p.req("R-WATCHDOG", "Watchdog", "The firmware shall stop stepping and raise an alarm when its control loop has not run for 10 ms.", refines=["CTRL.R-ESTOP-CIRCUIT"])
p.req("R-NO-AUTOSTART", "Starts in alarm", "The firmware shall start in the alarm state after every reset and shall move nothing until the operator clears it.", refines=["CTRL.R-NO-RESTART"])
p.req("R-EVENTS", "Events written", "The firmware shall write every alarm, reset, homing and program start and end to the event log with its time.", priority="could", refines=["CTRL.R-EVENT-LOG"])
p.heading("H-DES", "Design")
p.design("D-PLANNER", "Planner and step generator", "A ring of planned blocks feeds a timer interrupt that makes the step pulses. Corner speed comes from a junction deviation setting.", satisfies=["R-PLAN", "R-JITTER", "R-FEED-HOLD"])
p.design("D-STATES", "State machine", "Alarm, Idle, Home, Run, Hold and Probe. Every reset enters Alarm; only an operator command leaves it.", satisfies=["R-NO-AUTOSTART", "R-HOME-SEQ", "R-SOFT-LIMIT", "R-WATCHDOG"])
p.design("D-PARSER", "Line parser with a word table", "One table lists every accepted word and its modal group; a word that is not in the table is an error.", satisfies=["R-PARSE"])
p.design("D-CAPTURE", "Hardware capture for limits and probe", "Limit and probe inputs use the timer's capture channels, so the response does not wait for the main loop.", satisfies=["R-HARD-LIMIT", "R-PROBE-LATCH"])
p.design("D-PROTOCOL", "Line protocol over TCP", "Text commands in, status lines out, on one TCP connection. The status line carries state, position, feed, speed and alarm code.", satisfies=["R-STATUS-RATE", "R-EVENTS"])
p.heading("H-TEST", "Tests")
p.test("T-CONFORM", "G-code conformance suite", "Run 400 programs through the parser on the host and compare the planned moves with the reference. Pass: every accepted program matches and every rejected one names its line.", verifies=["R-PARSE"], execution="automated")
p.test("T-PLANNER", "Planner unit tests", "Unit tests for corner speed, look-ahead depth and soft limits on the host build. Pass: all pass.", verifies=["R-PLAN", "R-SOFT-LIMIT"], execution="automated")
p.test("T-JITTER", "Step jitter on the logic analyser", "Capture step outputs at 250 kHz for 60 s and compare with the plan. Pass: every edge within 2 µs.", verifies=["R-JITTER"])
p.test("T-CAPTURE", "Limit and probe latency", "Drive the limit and probe inputs from a signal generator and time the last step and the latch. Pass: 100 µs or less and 20 µs or less.", verifies=["R-HARD-LIMIT", "R-PROBE-LATCH"])
p.test("T-HIL", "Hardware-in-the-loop run", "On the test rig with simulated axes: home, run a program with 20 feed holds, reset mid-program, and stall the main loop. Pass: homing order as specified, no lost steps, alarm after every reset, and steps stop within 10 ms of the stall.", verifies=["R-HOME-SEQ", "R-FEED-HOLD", "R-NO-AUTOSTART", "R-WATCHDOG"], execution="automated")
p.test("T-STATUS", "Status and events", "Record the status stream and read back the event log after the hardware-in-the-loop run. Pass: no gap over 100 ms and every event present in order.", verifies=["R-STATUS-RATE", "R-EVENTS"], execution="automated")


# --------------------------------------------------------------------------
# Control software
# --------------------------------------------------------------------------
p = Project(
    "SW",
    "Mill Control Software",
    "SYS",
    "The application on the operator's computer: it loads a program, shows the toolpath and the machine's state, "
    "jogs, probes and starts the job.",
    owner="Software team",
)
p.heading("H-REQ", "Requirements")
p.req("R-PLATFORMS", "Where it runs", "The software shall run on Windows 10 and later, macOS 13 and later, and Ubuntu 22.04 and later.", refines=["SYS.R-GCODE"])
p.req("R-LOAD", "Loading a program", "The software shall load a G-code file of up to 50 MB and show its toolpath within 5 s.", refines=["SYS.R-GCODE"])
p.req("R-CHECK", "Check before start", "The software shall refuse to start a program that would leave the travel of the machine or that uses a word the controller does not accept, and shall name the first line at fault.", refines=["SYS.R-GCODE", "SYS.R-LIMITS"])
p.req("R-DISPLAY", "Live display", "The software shall show tool position, spindle speed, feed rate and machine state, updated five times a second or more.", "demonstration", refines=["SYS.R-STATUS"])
p.req("R-ALARM-TEXT", "Alarms in words", "The software shall show each alarm with what caused it and what the operator does next.", "inspection", refines=["SYS.R-STATUS"])
p.req("R-JOG", "Jogging", "The software shall jog each axis in steps of 0.01, 0.1, 1 and 10 mm and continuously while a key is held.", "demonstration", refines=["SYS.R-HOME"])
p.req("R-RESUME-UI", "Resume from a line", "The software shall let the operator resume a stopped program from the interrupted line and shall restore the spindle speed, coolant and offsets before the tool moves.", priority="should", refines=["SYS.R-RESUME"])
p.req("R-TOOL-TABLE", "Tool length probing", "The software shall run a tool length measurement and store the result in the tool table without the operator typing a number.", "demonstration", "should", refines=["SYS.R-PROBE"])
p.req("R-EXPORT-LOG", "Export the log", "The software shall export the controller's running hours and events as a CSV file.", "demonstration", "could", refines=["SYS.R-LOG"])
p.req("R-START-GUARD", "Cycle start", "The software shall start a cycle only from a dedicated control that the operator holds for one second.", "demonstration", refines=["SYS.R-DOOR"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-WRONG-OFFSET", "Start with the wrong work offset", "The operator starts a program with the offset of the last job. Harm: the tool drives into the vice; broken tool, ejected fragment.", "medium")
p.heading("H-DES", "Design")
p.design("D-APP", "Desktop application with a machine service", "One application for all three platforms. A background service owns the connection to the controller; the window can close without dropping a running job.", satisfies=["R-PLATFORMS", "R-DISPLAY", "R-JOG", "R-EXPORT-LOG"])
p.design("D-VIEWER", "Toolpath viewer", "Parses the program in a worker and draws the path in 3D with the machine's travel as a box around it.", satisfies=["R-LOAD", "R-CHECK"])
p.design("D-PREFLIGHT", "Pre-flight panel", "Before a start the software shows the program's extent against the travel, the active offset and the fitted tool, and the operator confirms each.", satisfies=["R-CHECK", "R-START-GUARD", "R-RESUME-UI"], mitigates=["HZ-WRONG-OFFSET"])
p.design("D-ROUTINES", "Probing routines", "Guided routines for tool length and for finding an edge or a corner, each writing its result to the tool or offset table.", satisfies=["R-TOOL-TABLE"])
p.design("D-ALARMS", "Alarm catalogue", "One file lists every alarm code with its cause and recovery; the software and the manual both read it.", satisfies=["R-ALARM-TEXT"])
p.heading("H-TEST", "Tests")
p.test("T-INSTALL", "Install matrix", "Install and connect to the simulator on Windows 10, Windows 11, macOS 13, macOS 15, Ubuntu 22.04 and Ubuntu 24.04. Pass: installs, connects and runs the sample program on all six.", verifies=["R-PLATFORMS"], execution="automated")
p.test("T-LOAD", "Large program", "Load a 50 MB program on the reference laptop ten times. Pass: toolpath shown within 5 s each time.", verifies=["R-LOAD"], execution="automated")
p.test("T-PREFLIGHT", "Pre-flight checks", "Try to start programs that leave the travel on each axis and programs with unknown words; try a short press on cycle start. Pass: every one refused with its line named, and the short press does nothing.", verifies=["R-CHECK", "R-START-GUARD"], execution="automated")
p.test("T-SIM", "Operation against the simulator", "Jog in each step size, run a program, hold, resume from a line, probe a tool and export the log. Pass: every step does what its requirement says, and the display updates five times a second.", verifies=["R-DISPLAY", "R-JOG", "R-RESUME-UI", "R-TOOL-TABLE", "R-EXPORT-LOG"], execution="mixed")
p.test("T-ALARM-REVIEW", "Alarm text review", "Raise every alarm on the simulator and read its text against the catalogue. Pass: each has a cause and a next step, and none shows only a code.", verifies=["R-ALARM-TEXT"])


# --------------------------------------------------------------------------
# Enclosure and safety
# --------------------------------------------------------------------------
p = Project(
    "ENC",
    "Mill Enclosure and Safety",
    "SYS",
    "The enclosure, the door and its interlock, the emergency stop station, the chip tray and the coolant "
    "containment of the Desktop CNC Mill.",
    owner="Mechanical team",
)
p.heading("H-REQ", "Requirements")
p.req("R-ENCLOSE", "Work zone enclosed", "The enclosure shall close the work zone on every side with no opening wider than 6 mm while the door is shut.", "inspection", refines=["SYS.R-CONTAIN", "SYS.R-DOOR"])
p.req("R-IMPACT", "Window impact", "The window shall stop a 20 g steel projectile at 40 m/s without the projectile passing through.", refines=["SYS.R-CONTAIN"])
p.req("R-INTERLOCK", "Interlock switch", "The door shall carry a coded interlock switch with two channels that both open before the door has opened 10 mm.", refines=["SYS.R-DOOR"])
p.req("R-ESTOP-BUTTON", "Emergency stop button", "The enclosure shall carry a red emergency stop button on a yellow ground that the operator can reach from the working position without opening the door.", "inspection", refines=["SYS.R-ESTOP"])
p.req("R-CHIPS", "Chip tray", "The enclosure shall hold 3 litres of chips in a tray the operator can remove without tools.", "demonstration", "should", refines=["SYS.R-CONTAIN"])
p.req("R-COOLANT", "Coolant", "The enclosure shall leak no coolant during 30 minutes of flood cooling at 2 litres a minute.", refines=["SYS.R-CONTAIN"])
p.req("R-ATTENUATE", "Noise reduction", "The closed enclosure shall reduce the sound level at 1 m by 10 dB or more.", refines=["SYS.R-NOISE"])
p.req("R-ENC-MASS", "Enclosure mass", "The enclosure shall weigh no more than 14 kg.", "inspection", refines=["SYS.R-MASS"])
p.req("R-ENC-SIZE", "Outside dimensions", "The enclosure shall measure no more than 700 mm wide, 600 mm deep and 800 mm high with the door shut.", "inspection", refines=["SYS.R-SIZE"])
p.req("R-LIGHT", "Lighting", "The enclosure shall light the table to 500 lux or more.", priority="could", refines=["SYS.R-STATUS"])
p.heading("H-HAZ", "Hazards")
p.hazard("HZ-DEFEAT", "Interlock defeated", "The operator holds the interlock closed with a magnet or tape to watch the cut with the door open. Harm: contact with the tool; eye injury.", "high")
p.hazard("HZ-WINDOW-AGE", "Window weakened by coolant", "Coolant makes the polycarbonate brittle over years, so it no longer stops a fragment. Harm: eye injury.", "medium")
p.heading("H-DES", "Design")
p.design("D-SHELL", "Sheet-steel shell with a lined interior", "A 1.2 mm steel shell lined with damping sheet, with labyrinth seals at the door and a sloped floor to the chip tray.", satisfies=["R-ENCLOSE", "R-ATTENUATE", "R-COOLANT", "R-CHIPS", "R-ENC-MASS", "R-ENC-SIZE"])
p.design("D-WINDOW", "Laminated window with a replace-by date", "A 6 mm polycarbonate window with a hard-coated inner sheet that takes the coolant, marked with the date it must be replaced.", satisfies=["R-IMPACT"], mitigates=["HZ-WINDOW-AGE"])
p.design("D-SWITCH", "Coded interlock", "A coded non-contact switch whose actuator is riveted to the door; a plain magnet does not operate it.", satisfies=["R-INTERLOCK"], mitigates=["HZ-DEFEAT"])
p.design("D-STATION", "Operator station", "The emergency stop, the reset button and the lighting switch on the front right of the enclosure, 150 mm above the bench.", satisfies=["R-ESTOP-BUTTON", "R-LIGHT"])
p.heading("H-TEST", "Tests")
p.test("T-IMPACT", "Window impact test", "Fire a 20 g steel projectile at 40 m/s at the centre and at a corner of three windows, one aged in coolant for 1,000 hours. Pass: no projectile passes through.", verifies=["R-IMPACT"])
p.test("T-INTERLOCK", "Interlock function and defeat", "Open the door slowly and record when each channel opens; try to operate the switch with a plain magnet and with tape. Pass: both channels open before 10 mm and neither defeat works.", verifies=["R-INTERLOCK"])
p.test("T-FLOOD", "Flood and chip test", "Run flood coolant at 2 litres a minute for 30 minutes while cutting; then remove the chip tray. Pass: no coolant outside, tray holds 3 litres, tray out without tools.", verifies=["R-COOLANT", "R-CHIPS"])
p.test("T-ATTENUATE", "Noise reduction measurement", "Measure sound level at 1 m with a reference source inside, door open and door shut. Pass: 10 dB or more lower with the door shut.", verifies=["R-ATTENUATE"])
p.test("T-ENC-INSPECT", "Enclosure inspection", "Probe every gap with a 6 mm rod, weigh and measure the enclosure, check the emergency stop for colour and reach, and measure light at the table. Pass: every figure within its requirement.", verifies=["R-ENCLOSE", "R-ENC-MASS", "R-ENC-SIZE", "R-ESTOP-BUTTON", "R-LIGHT"])
