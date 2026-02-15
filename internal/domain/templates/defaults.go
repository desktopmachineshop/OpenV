package templates

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/exports"
	linksdomain "github.com/openv/requirements-platform/internal/domain/links"
)

const cncMillTemplateKey = "example-cnc-mill"

// DefaultTemplates returns bundled templates.
func DefaultTemplates() ([]*Template, error) {
	exampleSnapshot, err := buildCncMillExampleSnapshot()
	if err != nil {
		return nil, err
	}

	return []*Template{
		{
			ID:          uuid.New().String(),
			Key:         cncMillTemplateKey,
			Name:        "Desktop CNC Mill Example",
			Description: "Example project with requirements for a desktop CNC mill.",
			Snapshot:    json.RawMessage(exampleSnapshot),
			IsDefault:   true,
			CreatedAt:   time.Now(),
		},
	}, nil
}

func buildCncMillExampleSnapshot() ([]byte, error) {
	now := time.Now()
	artifactsList := make([]*artifacts.Artifact, 0)
	linksList := make([]*linksdomain.Link, 0)

	newArtifact := func(id, parentID, artType, title, body string) *artifacts.Artifact {
		var parent *string
		if parentID != "" {
			parent = &parentID
		}
		return &artifacts.Artifact{
			ID:         id,
			ProjectID:  "example",
			ParentID:   parent,
			Type:       artType,
			Title:      title,
			Body:       body,
			Attributes: map[string]interface{}{},
			Version:    1,
			ValidFrom:  now,
			ValidTo:    nil,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}

	newLink := func(fromID, toID, linkType string) *linksdomain.Link {
		return &linksdomain.Link{
			ID:         uuid.New().String(),
			FromID:     fromID,
			ToID:       toID,
			Type:       linkType,
			Attributes: map[string]interface{}{},
			Version:    1,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	}

	heading1 := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(heading1, "", "heading", "Desktop CNC Mill", "Example project for a benchtop CNC milling machine."))

	overview := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(overview, heading1, "heading", "Overview", "System scope and operational overview."))
	artifactsList = append(artifactsList, newArtifact(uuid.New().String(), overview, "description", "Product summary", "The system is a desktop CNC mill for prototyping aluminum and plastic parts with a working envelope suitable for small components."))
	artifactsList = append(artifactsList, newArtifact(uuid.New().String(), overview, "description", "Stakeholders", "Primary users include makers, prototyping engineers, and small lab technicians."))

	mechanical := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(mechanical, heading1, "heading", "Mechanical Subsystem", "Structure, motion, and spindle requirements."))
	rigidFrameID := uuid.New().String()
	travelLimitsID := uuid.New().String()
	xTravelID := uuid.New().String()
	yTravelID := uuid.New().String()
	zTravelID := uuid.New().String()
	spindleSpeedID := uuid.New().String()
	spindleAssemblyID := uuid.New().String()
	toolHoldingID := uuid.New().String()
	runoutID := uuid.New().String()
	ballScrewID := uuid.New().String()
	linearRailsID := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(rigidFrameID, mechanical, "requirement", "Rigid frame", "The frame shall maintain tool position within 0.05 mm under full load to ensure repeatable cuts."))
	artifactsList = append(artifactsList, newArtifact(travelLimitsID, mechanical, "requirement", "Travel limits", "The X axis travel shall be at least 300 mm, Y axis at least 200 mm, and Z axis at least 150 mm."))
	artifactsList = append(artifactsList, newArtifact(xTravelID, mechanical, "requirement", "X axis travel", "The X axis travel shall be at least 300 mm."))
	artifactsList = append(artifactsList, newArtifact(yTravelID, mechanical, "requirement", "Y axis travel", "The Y axis travel shall be at least 200 mm."))
	artifactsList = append(artifactsList, newArtifact(zTravelID, mechanical, "requirement", "Z axis travel", "The Z axis travel shall be at least 150 mm."))
	artifactsList = append(artifactsList, newArtifact(spindleSpeedID, mechanical, "requirement", "Spindle speed", "The spindle shall support speeds from 5,000 to 20,000 RPM."))
	artifactsList = append(artifactsList, newArtifact(spindleAssemblyID, mechanical, "design-item", "Spindle assembly design", "Design the spindle assembly to meet speed and runout requirements."))
	artifactsList = append(artifactsList, newArtifact(toolHoldingID, mechanical, "requirement", "Tool holding", "The spindle shall accept ER11 collets for tool diameters from 1 mm to 7 mm."))
	artifactsList = append(artifactsList, newArtifact(runoutID, mechanical, "requirement", "Runout", "Spindle runout shall be less than 0.02 mm at the tool tip."))
	artifactsList = append(artifactsList, newArtifact(ballScrewID, mechanical, "requirement", "Ball screw drive", "All motion axes shall use ball screws with anti-backlash nuts."))
	artifactsList = append(artifactsList, newArtifact(linearRailsID, mechanical, "requirement", "Linear rails", "Each axis shall use linear rails rated for at least 1000 N load."))

	electrical := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(electrical, heading1, "heading", "Electrical Subsystem", "Motors, power, and controls."))
	motorTorqueID := uuid.New().String()
	powerSupplyID := uuid.New().String()
	emergencyStopID := uuid.New().String()
	limitSwitchesID := uuid.New().String()
	cableManagementID := uuid.New().String()
	controlEnclosureID := uuid.New().String()
	noiseImmunityID := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(motorTorqueID, electrical, "requirement", "Motor torque", "Each axis motor shall provide at least 1.2 Nm holding torque."))
	artifactsList = append(artifactsList, newArtifact(powerSupplyID, electrical, "requirement", "Power supply", "The system shall operate from a 120/240 VAC input with an internal 24 VDC rail."))
	artifactsList = append(artifactsList, newArtifact(emergencyStopID, electrical, "requirement", "Emergency stop", "An E-stop shall cut spindle power and disable axis drives within 100 ms."))
	artifactsList = append(artifactsList, newArtifact(limitSwitchesID, electrical, "requirement", "Limit switches", "Each axis shall include homing and limit switches to prevent overtravel."))
	artifactsList = append(artifactsList, newArtifact(cableManagementID, electrical, "requirement", "Cable management", "Cable chains shall protect wiring and maintain bend radius above 30 mm."))
	artifactsList = append(artifactsList, newArtifact(controlEnclosureID, electrical, "requirement", "Control enclosure", "All electronics shall be housed in an enclosure rated at least IP20."))
	artifactsList = append(artifactsList, newArtifact(noiseImmunityID, electrical, "requirement", "Noise immunity", "Signal lines shall be shielded and grounded to prevent EMI induced errors."))

	software := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(software, heading1, "heading", "Control Software", "Motion control and user interface."))
	gcodeSupportID := uuid.New().String()
	jogControlsID := uuid.New().String()
	feedOverrideID := uuid.New().String()
	spindleControlID := uuid.New().String()
	jobResumeID := uuid.New().String()
	fileImportID := uuid.New().String()
	calibrationID := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(gcodeSupportID, software, "requirement", "G-code support", "The controller shall interpret standard G-code commands including G0, G1, G2, and G3."))
	artifactsList = append(artifactsList, newArtifact(jogControlsID, software, "requirement", "Jog controls", "The UI shall provide manual jog controls with selectable step sizes."))
	artifactsList = append(artifactsList, newArtifact(feedOverrideID, software, "requirement", "Feed override", "The operator shall be able to override feed rate between 50% and 150% during a job."))
	artifactsList = append(artifactsList, newArtifact(spindleControlID, software, "requirement", "Spindle control", "The UI shall allow spindle speed changes during a program pause."))
	artifactsList = append(artifactsList, newArtifact(jobResumeID, software, "requirement", "Job resume", "The system shall resume interrupted jobs from the last safe tool position."))
	artifactsList = append(artifactsList, newArtifact(fileImportID, software, "requirement", "File import", "The controller shall accept jobs from USB and network sources."))
	artifactsList = append(artifactsList, newArtifact(calibrationID, software, "requirement", "Calibration", "The system shall provide a calibration routine for steps-per-mm on each axis."))

	safety := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(safety, heading1, "heading", "Safety Requirements", "Operational safety and compliance."))
	guardingID := uuid.New().String()
	flyingDebrisHazardID := uuid.New().String()
	doorInterlockID := uuid.New().String()
	overcurrentProtectionID := uuid.New().String()
	thermalMonitoringID := uuid.New().String()
	emergencyInstructionsID := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(guardingID, safety, "requirement", "Guarding", "The cutting area shall include a transparent guard to reduce exposure to chips."))
	artifactsList = append(artifactsList, newArtifact(flyingDebrisHazardID, safety, "hazard", "Flying debris exposure", "Operators may be exposed to chips and debris during cutting operations."))
	artifactsList = append(artifactsList, newArtifact(doorInterlockID, safety, "requirement", "Door interlock", "The spindle shall stop within 1 second if the guard door is opened."))
	artifactsList = append(artifactsList, newArtifact(overcurrentProtectionID, safety, "requirement", "Overcurrent protection", "The system shall include fusing to prevent damage from overcurrent conditions."))
	artifactsList = append(artifactsList, newArtifact(thermalMonitoringID, safety, "requirement", "Thermal monitoring", "Spindle temperature shall be monitored and trigger shutdown above 80C."))
	artifactsList = append(artifactsList, newArtifact(emergencyInstructionsID, safety, "requirement", "Emergency instructions", "The UI shall display emergency stop and safe shutdown instructions."))

	quality := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(quality, heading1, "heading", "Performance Requirements", "Accuracy and throughput."))
	positionAccuracyID := uuid.New().String()
	repeatabilityID := uuid.New().String()
	surfaceFinishID := uuid.New().String()
	continuousDutyID := uuid.New().String()
	cuttingFeedID := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(positionAccuracyID, quality, "requirement", "Position accuracy", "The system shall achieve positional accuracy within 0.02 mm."))
	artifactsList = append(artifactsList, newArtifact(repeatabilityID, quality, "requirement", "Repeatability", "The system shall have repeatability within 0.01 mm across the working envelope."))
	artifactsList = append(artifactsList, newArtifact(surfaceFinishID, quality, "requirement", "Surface finish", "The system shall achieve surface finishes of Ra 1.6 or better on aluminum."))
	artifactsList = append(artifactsList, newArtifact(continuousDutyID, quality, "requirement", "Continuous duty", "The system shall support continuous milling for 2 hours without thermal drift beyond 0.01 mm."))
	artifactsList = append(artifactsList, newArtifact(cuttingFeedID, quality, "requirement", "Cutting feed", "The system shall support cutting feed rates up to 1500 mm/min."))

	verification := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(verification, heading1, "heading", "Verification", "Planned verification activities."))
	frameStiffnessTestID := uuid.New().String()
	travelMeasurementTestID := uuid.New().String()
	estopResponseTestID := uuid.New().String()
	spindleRunoutTestID := uuid.New().String()
	jobResumeTestID := uuid.New().String()
	thermalShutdownTestID := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(frameStiffnessTestID, verification, "test-case", "Frame stiffness test", "Apply maximum load to the spindle mount and verify deflection is within 0.05 mm."))
	artifactsList = append(artifactsList, newArtifact(travelMeasurementTestID, verification, "test-case", "Travel measurement", "Measure axis travel and confirm minimum travel distances are met."))
	artifactsList = append(artifactsList, newArtifact(estopResponseTestID, verification, "test-case", "E-stop response", "Trigger E-stop during operation and verify power cut within 100 ms."))
	artifactsList = append(artifactsList, newArtifact(spindleRunoutTestID, verification, "test-case", "Spindle runout test", "Measure runout using a dial indicator and verify below 0.02 mm."))
	artifactsList = append(artifactsList, newArtifact(jobResumeTestID, verification, "test-case", "Job resume test", "Interrupt a job, resume, and verify no visible step in the cut path."))
	artifactsList = append(artifactsList, newArtifact(thermalShutdownTestID, verification, "test-case", "Thermal shutdown", "Raise spindle temperature and confirm shutdown at 80C."))

	additional := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(additional, heading1, "heading", "Additional Requirements", "Secondary and support requirements."))
	maintenanceScheduleID := uuid.New().String()
	toolLibraryID := uuid.New().String()
	noiseLevelID := uuid.New().String()
	dustManagementID := uuid.New().String()
	installationID := uuid.New().String()
	userManualID := uuid.New().String()
	artifactsList = append(artifactsList, newArtifact(maintenanceScheduleID, additional, "requirement", "Maintenance schedule", "The system shall provide a maintenance schedule for lubrication and alignment."))
	artifactsList = append(artifactsList, newArtifact(toolLibraryID, additional, "requirement", "Tool library", "The controller shall allow storage of at least 20 tool profiles."))
	artifactsList = append(artifactsList, newArtifact(noiseLevelID, additional, "requirement", "Noise level", "The machine shall operate below 75 dBA at 1 meter."))
	artifactsList = append(artifactsList, newArtifact(dustManagementID, additional, "requirement", "Dust management", "The system shall provide a dust shoe compatible with standard vacuum hoses."))
	artifactsList = append(artifactsList, newArtifact(installationID, additional, "requirement", "Installation", "The machine shall be installable by two people in under 2 hours."))
	artifactsList = append(artifactsList, newArtifact(userManualID, additional, "requirement", "User manual", "A user manual shall be provided covering setup, operation, and troubleshooting."))

	linksList = append(linksList,
		newLink(frameStiffnessTestID, rigidFrameID, "verifies"),
		newLink(travelMeasurementTestID, travelLimitsID, "verifies"),
		newLink(estopResponseTestID, emergencyStopID, "verifies"),
		newLink(spindleRunoutTestID, runoutID, "verifies"),
		newLink(jobResumeTestID, jobResumeID, "verifies"),
		newLink(thermalShutdownTestID, thermalMonitoringID, "verifies"),
		newLink(spindleAssemblyID, spindleSpeedID, "satisfies"),
		newLink(guardingID, flyingDebrisHazardID, "mitigates"),
		newLink(controlEnclosureID, noiseImmunityID, "relates-to"),
		newLink(travelLimitsID, xTravelID, "decomposes-to"),
		newLink(travelLimitsID, yTravelID, "decomposes-to"),
		newLink(travelLimitsID, zTravelID, "decomposes-to"),
		newLink(spindleSpeedID, surfaceFinishID, "impacts"),
	)

	for len(artifactsList) < 50 {
		artifactsList = append(artifactsList, newArtifact(uuid.New().String(), additional, "requirement", fmt.Sprintf("Supplemental requirement %d", len(artifactsList)-45), "Additional requirement to reach the target artifact count."))
	}

	exportData := exports.ProjectExport{
		ExportedAt:  now,
		Version:     "1.0",
		ProjectID:   "example",
		ProjectName: "Desktop CNC Mill",
		ProjectDesc: "Example project for a desktop CNC mill with requirements, safety, and verification artifacts.",
		Artifacts:   artifactsList,
		Links:       linksList,
		Attachments: []*attachments.Attachment{},
	}

	return json.MarshalIndent(exportData, "", "  ")
}
