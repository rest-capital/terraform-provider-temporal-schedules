package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

const fingerprintKey = "fingerprint"

var (
	_ resource.Resource                = &scheduleResource{}
	_ resource.ResourceWithImportState = &scheduleResource{}
)

// isNotFoundError returns true if the error is a Temporal NotFound service error.
func isNotFoundError(err error) bool {
	var notFound *serviceerror.NotFound
	return errors.As(err, &notFound)
}

type scheduleResource struct{}

type scheduleResourceModel struct {
	Namespace     types.String            `tfsdk:"namespace"`
	Address       types.String            `tfsdk:"address"`
	APIKey        types.String            `tfsdk:"api_key"`
	Name          types.String            `tfsdk:"name"`
	Action        *actionModel            `tfsdk:"action"`
	Spec          *specModel              `tfsdk:"spec"`
	OverlapPolicy types.String            `tfsdk:"overlap_policy"`
	IsPaused      types.Bool              `tfsdk:"is_paused"`
	CatchupWindow types.String            `tfsdk:"catchup_window"`
	Memo          map[string]types.String `tfsdk:"memo"`
	DriftDetected types.Bool              `tfsdk:"drift_detected"`
}

type actionModel struct {
	WorkflowType types.String `tfsdk:"workflow_type"`
	TaskQueue    types.String `tfsdk:"task_queue"`
	WorkflowID   types.String `tfsdk:"workflow_id"`
	InputPayload types.String `tfsdk:"input_payload"`
}

type specModel struct {
	CronExpressions []types.String  `tfsdk:"cron_expressions"`
	Calendars       []calendarModel `tfsdk:"calendar"`
	Intervals       []intervalModel `tfsdk:"interval"`
	Timezone        types.String    `tfsdk:"timezone"`
	Jitter          types.String    `tfsdk:"jitter"`
	StartAt         types.String    `tfsdk:"start_at"`
	EndAt           types.String    `tfsdk:"end_at"`
}

type calendarModel struct {
	Year       types.String `tfsdk:"year"`
	Month      types.String `tfsdk:"month"`
	DayOfMonth types.String `tfsdk:"day_of_month"`
	DayOfWeek  types.String `tfsdk:"day_of_week"`
	Hour       types.String `tfsdk:"hour"`
	Minute     types.String `tfsdk:"minute"`
	Second     types.String `tfsdk:"second"`
	Comment    types.String `tfsdk:"comment"`
}

type intervalModel struct {
	Every  types.String `tfsdk:"every"`
	Offset types.String `tfsdk:"offset"`
}

func NewScheduleResource() resource.Resource {
	return &scheduleResource{}
}

func (r *scheduleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schedule"
}

func (r *scheduleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Temporal Schedule.",
		Attributes: map[string]schema.Attribute{
			"namespace": schema.StringAttribute{
				Required:    true,
				Description: "Temporal namespace.",
			},
			"address": schema.StringAttribute{
				Required:    true,
				Description: "Temporal gRPC endpoint address.",
			},
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Temporal API key. Required for Temporal Cloud.",
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Schedule identifier.",
			},
			"overlap_policy": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("skip"),
				Description: "Overlap policy: skip, buffer_one, buffer_all, cancel_other, terminate_other, allow_all.",
				Validators: []validator.String{
					stringvalidator.OneOf(validOverlapPolicies...),
				},
			},
			"is_paused": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether the schedule is paused.",
			},
			"catchup_window": schema.StringAttribute{
				Optional:    true,
				Description: "Catchup window duration (e.g. '365d', '1h').",
			},
			"memo": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Memo key-value pairs.",
			},
			// Planned as false, so a true from Read forces an update.
			"drift_detected": schema.BoolAttribute{
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "True when the schedule was changed outside Terraform since the last apply. The next apply overwrites it from configuration. Pausing, unpausing and notes are not counted.",
			},
		},
		Blocks: map[string]schema.Block{
			"action": schema.SingleNestedBlock{
				Description: "Workflow action to execute.",
				Attributes: map[string]schema.Attribute{
					"workflow_type": schema.StringAttribute{
						Required:    true,
						Description: "Workflow type name.",
					},
					"task_queue": schema.StringAttribute{
						Required:    true,
						Description: "Task queue name.",
					},
					"workflow_id": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Description: "Workflow execution ID prefix. Temporal appends a timestamp suffix to this value. Defaults to the schedule name.",
						Validators: []validator.String{
							stringvalidator.LengthAtMost(979),
						},
					},
					"input_payload": schema.StringAttribute{
						Optional:    true,
						Description: "JSON-encoded workflow input.",
					},
				},
			},
			"spec": schema.SingleNestedBlock{
				Description: "Schedule specification.",
				Attributes: map[string]schema.Attribute{
					"cron_expressions": schema.ListAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Cron expressions.",
					},
					"timezone": schema.StringAttribute{
						Optional:    true,
						Description: "IANA timezone name.",
					},
					"jitter": schema.StringAttribute{
						Optional:    true,
						Description: "Random jitter duration.",
					},
					"start_at": schema.StringAttribute{
						Optional:    true,
						Description: "Start time (RFC3339).",
					},
					"end_at": schema.StringAttribute{
						Optional:    true,
						Description: "End time (RFC3339).",
					},
				},
				Blocks: map[string]schema.Block{
					"calendar": schema.ListNestedBlock{
						Description: "Calendar-based schedule spec.",
						NestedObject: schema.NestedBlockObject{
							Attributes: map[string]schema.Attribute{
								"year":         schema.StringAttribute{Optional: true, Description: "Year range."},
								"month":        schema.StringAttribute{Optional: true, Description: "Month range (1-12)."},
								"day_of_month": schema.StringAttribute{Optional: true, Description: "Day of month range (1-31)."},
								"day_of_week":  schema.StringAttribute{Optional: true, Description: "Day of week range (0-6, 0=Sunday)."},
								"hour":         schema.StringAttribute{Optional: true, Description: "Hour range (0-23)."},
								"minute":       schema.StringAttribute{Optional: true, Description: "Minute range (0-59)."},
								"second":       schema.StringAttribute{Optional: true, Description: "Second range (0-59)."},
								"comment":      schema.StringAttribute{Optional: true, Description: "Description of this calendar spec."},
							},
						},
					},
					"interval": schema.ListNestedBlock{
						Description: "Interval-based schedule spec.",
						NestedObject: schema.NestedBlockObject{
							Attributes: map[string]schema.Attribute{
								"every":  schema.StringAttribute{Required: true, Description: "Repeat interval (e.g. '4h', '30m')."},
								"offset": schema.StringAttribute{Optional: true, Description: "Offset from the interval (e.g. '5m')."},
							},
						},
					},
				},
			},
		},
	}
}

func (r *scheduleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan scheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	scheduleSpec, diags := buildScheduleSpec(plan.Spec)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	action, diags := buildWorkflowAction(plan.Action, plan.Name.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := client.ScheduleOptions{
		ID:      plan.Name.ValueString(),
		Spec:    scheduleSpec,
		Action:  action,
		Overlap: toOverlapPolicy(plan.OverlapPolicy.ValueString()),
		Paused:  plan.IsPaused.ValueBool(),
	}

	if !plan.CatchupWindow.IsNull() && !plan.CatchupWindow.IsUnknown() {
		d, err := parseDuration(plan.CatchupWindow.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid catchup_window", err.Error())
			return
		}
		opts.CatchupWindow = d
	}

	if plan.Memo != nil {
		opts.Memo = make(map[string]interface{}, len(plan.Memo))
		for k, v := range plan.Memo {
			opts.Memo[k] = v.ValueString()
		}
	}

	err := withRetryableClient(ctx, plan.Address.ValueString(), plan.Namespace.ValueString(), plan.APIKey.ValueString(),
		func(tc client.Client) error {
			_, createErr := tc.ScheduleClient().Create(ctx, opts)
			return createErr
		})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(storeFingerprint(ctx, resp.Private, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state scheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.Address.IsNull() || state.Address.ValueString() == "" {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		return
	}

	var desc *client.ScheduleDescription
	err := withRetryableClient(ctx, state.Address.ValueString(), state.Namespace.ValueString(), state.APIKey.ValueString(),
		func(tc client.Client) error {
			handle := tc.ScheduleClient().GetHandle(ctx, state.Name.ValueString())
			var descErr error
			desc, descErr = handle.Describe(ctx)
			return descErr
		})
	if err != nil {
		if isNotFoundError(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to describe schedule", err.Error())
		return
	}

	readScheduleIntoState(&state, desc)

	live, err := scheduleFingerprint(desc)
	if err != nil {
		resp.Diagnostics.AddError("Failed to fingerprint schedule", err.Error())
		return
	}
	stored, diags := req.Private.GetKey(ctx, fingerprintKey)
	resp.Diagnostics.Append(diags...)
	switch {
	case bytes.Equal(stored, live):
		state.DriftDetected = types.BoolValue(false)
	case !sameDeps(stored):
		resp.Diagnostics.Append(resp.Private.SetKey(ctx, fingerprintKey, live)...)
		state.DriftDetected = types.BoolValue(false)
	default:
		state.DriftDetected = types.BoolValue(true)
		resp.Diagnostics.AddWarning("Schedule changed outside Terraform",
			fmt.Sprintf("Schedule %q in namespace %q was changed outside Terraform. The next apply will overwrite it with the configuration.",
				state.Name.ValueString(), state.Namespace.ValueString()))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *scheduleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan scheduleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	scheduleSpec, diags := buildScheduleSpec(plan.Spec)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	action, diags := buildWorkflowAction(plan.Action, plan.Name.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	overlapPolicy := toOverlapPolicy(plan.OverlapPolicy.ValueString())

	var catchupWindow time.Duration
	if !plan.CatchupWindow.IsNull() && !plan.CatchupWindow.IsUnknown() {
		var parseErr error
		catchupWindow, parseErr = parseDuration(plan.CatchupWindow.ValueString())
		if parseErr != nil {
			resp.Diagnostics.AddError("Invalid catchup_window", parseErr.Error())
			return
		}
	}

	err := withRetryableClient(ctx, plan.Address.ValueString(), plan.Namespace.ValueString(), plan.APIKey.ValueString(),
		func(tc client.Client) error {
			handle := tc.ScheduleClient().GetHandle(ctx, plan.Name.ValueString())
			return handle.Update(ctx, client.ScheduleUpdateOptions{
				DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
					// Keep note and limits; only paused is managed.
					var scheduleState client.ScheduleState
					if input.Description.Schedule.State != nil {
						scheduleState = *input.Description.Schedule.State
					}
					scheduleState.Paused = plan.IsPaused.ValueBool()
					return &client.ScheduleUpdate{
						Schedule: &client.Schedule{
							Action: action,
							Spec:   &scheduleSpec,
							Policy: &client.SchedulePolicies{
								Overlap:       overlapPolicy,
								CatchupWindow: catchupWindow,
							},
							State: &scheduleState,
						},
					}, nil
				},
			})
		})
	if err != nil {
		resp.Diagnostics.AddError("Failed to update schedule", err.Error())
		return
	}

	resp.Diagnostics.Append(storeFingerprint(ctx, resp.Private, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *scheduleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state scheduleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := withRetryableClient(ctx, state.Address.ValueString(), state.Namespace.ValueString(), state.APIKey.ValueString(),
		func(tc client.Client) error {
			handle := tc.ScheduleClient().GetHandle(ctx, state.Name.ValueString())
			return handle.Delete(ctx)
		})
	if err != nil {
		if isNotFoundError(err) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete schedule", err.Error())
		return
	}
}

func (r *scheduleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 {
		resp.Diagnostics.AddError("Invalid import ID", "Expected format: namespace/schedule-name")
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
}

func buildScheduleSpec(spec *specModel) (client.ScheduleSpec, diag.Diagnostics) {
	var result client.ScheduleSpec
	var diags diag.Diagnostics

	if spec == nil {
		return result, diags
	}

	for _, expr := range spec.CronExpressions {
		result.CronExpressions = append(result.CronExpressions, expr.ValueString())
	}

	for _, cal := range spec.Calendars {
		calSpec := client.ScheduleCalendarSpec{
			Comment: cal.Comment.ValueString(),
		}
		if !cal.Second.IsNull() && !cal.Second.IsUnknown() {
			calSpec.Second = parseRangeString(cal.Second.ValueString())
		}
		if !cal.Minute.IsNull() && !cal.Minute.IsUnknown() {
			calSpec.Minute = parseRangeString(cal.Minute.ValueString())
		}
		if !cal.Hour.IsNull() && !cal.Hour.IsUnknown() {
			calSpec.Hour = parseRangeString(cal.Hour.ValueString())
		}
		if !cal.DayOfMonth.IsNull() && !cal.DayOfMonth.IsUnknown() {
			calSpec.DayOfMonth = parseRangeString(cal.DayOfMonth.ValueString())
		}
		if !cal.Month.IsNull() && !cal.Month.IsUnknown() {
			calSpec.Month = parseRangeString(cal.Month.ValueString())
		}
		if !cal.Year.IsNull() && !cal.Year.IsUnknown() {
			calSpec.Year = parseRangeString(cal.Year.ValueString())
		}
		if !cal.DayOfWeek.IsNull() && !cal.DayOfWeek.IsUnknown() {
			calSpec.DayOfWeek = parseRangeString(cal.DayOfWeek.ValueString())
		}
		result.Calendars = append(result.Calendars, calSpec)
	}

	for _, intv := range spec.Intervals {
		every, err := parseDuration(intv.Every.ValueString())
		if err != nil {
			diags.AddError("Invalid interval.every", err.Error())
			return result, diags
		}
		interval := client.ScheduleIntervalSpec{Every: every}
		if !intv.Offset.IsNull() && !intv.Offset.IsUnknown() {
			offset, err := parseDuration(intv.Offset.ValueString())
			if err != nil {
				diags.AddError("Invalid interval.offset", err.Error())
				return result, diags
			}
			interval.Offset = offset
		}
		result.Intervals = append(result.Intervals, interval)
	}

	if !spec.Timezone.IsNull() && !spec.Timezone.IsUnknown() {
		result.TimeZoneName = spec.Timezone.ValueString()
	}

	if !spec.Jitter.IsNull() && !spec.Jitter.IsUnknown() {
		jitter, err := parseDuration(spec.Jitter.ValueString())
		if err != nil {
			diags.AddError("Invalid jitter", err.Error())
			return result, diags
		}
		result.Jitter = jitter
	}

	if !spec.StartAt.IsNull() && !spec.StartAt.IsUnknown() {
		t, err := time.Parse(time.RFC3339, spec.StartAt.ValueString())
		if err != nil {
			diags.AddError("Invalid start_at", err.Error())
			return result, diags
		}
		result.StartAt = t
	}

	if !spec.EndAt.IsNull() && !spec.EndAt.IsUnknown() {
		t, err := time.Parse(time.RFC3339, spec.EndAt.ValueString())
		if err != nil {
			diags.AddError("Invalid end_at", err.Error())
			return result, diags
		}
		result.EndAt = t
	}

	return result, diags
}

func buildWorkflowAction(action *actionModel, scheduleName string) (*client.ScheduleWorkflowAction, diag.Diagnostics) {
	var diags diag.Diagnostics
	if action == nil {
		diags.AddError("Missing action block", "An action block is required.")
		return nil, diags
	}

	wfAction := &client.ScheduleWorkflowAction{
		Workflow:  action.WorkflowType.ValueString(),
		TaskQueue: action.TaskQueue.ValueString(),
	}

	if !action.WorkflowID.IsNull() && !action.WorkflowID.IsUnknown() {
		wfAction.ID = action.WorkflowID.ValueString()
	} else {
		wfAction.ID = scheduleName
		action.WorkflowID = types.StringValue(scheduleName)
	}

	if !action.InputPayload.IsNull() && !action.InputPayload.IsUnknown() {
		var payload interface{}
		if err := json.Unmarshal([]byte(action.InputPayload.ValueString()), &payload); err != nil {
			diags.AddError("Invalid input_payload", fmt.Sprintf("Must be valid JSON: %s", err))
			return nil, diags
		}
		wfAction.Args = []interface{}{payload}
	}

	return wfAction, diags
}

func readScheduleIntoState(state *scheduleResourceModel, desc *client.ScheduleDescription) {
	if wfAction, ok := desc.Schedule.Action.(*client.ScheduleWorkflowAction); ok {
		if state.Action == nil {
			state.Action = &actionModel{}
		}
		state.Action.WorkflowType = types.StringValue(wfAction.Workflow.(string))
		state.Action.TaskQueue = types.StringValue(wfAction.TaskQueue)
		if wfAction.ID != "" {
			state.Action.WorkflowID = types.StringValue(wfAction.ID)
		}
	}

	if desc.Schedule.State != nil {
		state.IsPaused = types.BoolValue(desc.Schedule.State.Paused)
	}

	if desc.Schedule.Policy != nil {
		state.OverlapPolicy = types.StringValue(fromOverlapPolicy(desc.Schedule.Policy.Overlap))
	}
}

type fingerprint struct {
	Deps string `json:"deps"` // SDK conversion is only stable within one library version
	Hash string `json:"hash"`
}

var fingerprintDeps = func() string {
	var deps []string
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "go.temporal.io/sdk" || d.Path == "go.temporal.io/api" {
				deps = append(deps, d.Path+"@"+d.Version)
			}
		}
	}
	return strings.Join(deps, " ")
}()

// Temporal canonicalises specs, so compare fingerprints only with each other; State is excluded so pause is not drift.
func scheduleFingerprint(desc *client.ScheduleDescription) ([]byte, error) {
	managed := desc.Schedule
	managed.State = nil
	b, err := json.Marshal(managed)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	return json.Marshal(fingerprint{Deps: fingerprintDeps, Hash: hex.EncodeToString(sum[:])})
}

func sameDeps(stored []byte) bool {
	var fp fingerprint
	return json.Unmarshal(stored, &fp) == nil && fp.Deps == fingerprintDeps
}

type privateStateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

func storeFingerprint(ctx context.Context, private privateStateSetter, m *scheduleResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	var desc *client.ScheduleDescription
	err := withRetryableClient(ctx, m.Address.ValueString(), m.Namespace.ValueString(), m.APIKey.ValueString(),
		func(tc client.Client) error {
			var descErr error
			desc, descErr = tc.ScheduleClient().GetHandle(ctx, m.Name.ValueString()).Describe(ctx)
			return descErr
		})
	var fp []byte
	if err == nil {
		fp, err = scheduleFingerprint(desc)
	}
	if err != nil {
		diags.AddWarning("Failed to fingerprint schedule",
			fmt.Sprintf("Drift detection for schedule %q restarts from its state at the next refresh: %s", m.Name.ValueString(), err))
		fp = nil
	}
	diags.Append(private.SetKey(ctx, fingerprintKey, fp)...)
	return diags
}

func toOverlapPolicy(s string) enumspb.ScheduleOverlapPolicy {
	switch s {
	case "skip":
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	case "buffer_one":
		return enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE
	case "buffer_all":
		return enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL
	case "cancel_other":
		return enumspb.SCHEDULE_OVERLAP_POLICY_CANCEL_OTHER
	case "terminate_other":
		return enumspb.SCHEDULE_OVERLAP_POLICY_TERMINATE_OTHER
	case "allow_all":
		return enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL
	default:
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	}
}

func fromOverlapPolicy(p enumspb.ScheduleOverlapPolicy) string {
	switch p {
	case enumspb.SCHEDULE_OVERLAP_POLICY_SKIP:
		return "skip"
	case enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE:
		return "buffer_one"
	case enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL:
		return "buffer_all"
	case enumspb.SCHEDULE_OVERLAP_POLICY_CANCEL_OTHER:
		return "cancel_other"
	case enumspb.SCHEDULE_OVERLAP_POLICY_TERMINATE_OTHER:
		return "terminate_other"
	case enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL:
		return "allow_all"
	default:
		return "skip"
	}
}

func parseRangeString(s string) []client.ScheduleRange {
	if s == "" || s == "*" {
		return nil
	}

	var ranges []client.ScheduleRange
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		r := client.ScheduleRange{}
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			r.Start, _ = strconv.Atoi(strings.TrimSpace(bounds[0]))
			r.End, _ = strconv.Atoi(strings.TrimSpace(bounds[1]))
		} else {
			val, _ := strconv.Atoi(part)
			r.Start = val
			r.End = val
		}
		ranges = append(ranges, r)
	}
	return ranges
}
