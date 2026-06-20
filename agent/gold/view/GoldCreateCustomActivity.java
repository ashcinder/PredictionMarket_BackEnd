package com.example.brokerfi.xc.agent.gold.view;

import android.app.AlertDialog;
import android.app.DatePickerDialog;
import android.os.Bundle;
import android.view.LayoutInflater;
import android.view.View;
import android.widget.ArrayAdapter;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.Spinner;
import android.widget.TextView;
import android.widget.Toast;
import android.util.Log;
import androidx.appcompat.app.AppCompatActivity;
import androidx.lifecycle.ViewModelProvider;

import com.example.brokerfi.R;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.viewmodel.GoldCreatePoolViewModel;

import java.text.SimpleDateFormat;
import java.util.Arrays;
import java.util.Calendar;
import java.util.Date;
import java.util.List;
import java.util.Locale;

public class GoldCreateCustomActivity extends AppCompatActivity {
    private GoldCreatePoolViewModel viewModel;
    private String templateType, templateTitle;
    private Calendar startCalendar = Calendar.getInstance(), endCalendar = Calendar.getInstance();
    private boolean startSelected = false, endSelected = false;
    private TextView tvTemplateName, tvTemplateDetail;
    private EditText etParam1, etInitialLiquidity;
    private Button btnSelectStartTime, btnSelectTime, btnDeploy;
    private LinearLayout containerTechnical, containerDirection;
    private Spinner spinnerIndicator, spinnerOperator, spinnerDirection;
    private SimpleDateFormat dateFormat = new SimpleDateFormat("yyyy-MM-dd", Locale.getDefault());

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_gold_create_custom);
        templateType = getIntent().getStringExtra("TEMPLATE_TYPE");
        templateTitle = getIntent().getStringExtra("TEMPLATE_TITLE");
        viewModel = new ViewModelProvider(this).get(GoldCreatePoolViewModel.class);
        initViews();
        setupTemplateUI();
        observeViewModel();
    }

    private void observeViewModel() {
        viewModel.getIsDeploying().observe(this, deploying -> {
            btnDeploy.setEnabled(!deploying);
            btnDeploy.setText(deploying ? "正在部署..." : "部署博弈池");
        });
        viewModel.getTxStatus().observe(this, status -> {
            if (status != null) {
                Toast.makeText(this, status, Toast.LENGTH_LONG).show();
                if (status.startsWith("Success")) finish();
            }
        });
        viewModel.getError().observe(this, err -> {
            if (err != null) Toast.makeText(this, "部署失败: " + err, Toast.LENGTH_LONG).show();
        });
    }

    private void initViews() {
        findViewById(R.id.btn_back).setOnClickListener(v -> finish());
        tvTemplateName = findViewById(R.id.tv_template_name);
        tvTemplateDetail = findViewById(R.id.tv_template_detail);
        etParam1 = findViewById(R.id.et_param1);
        etInitialLiquidity = findViewById(R.id.et_initial_liquidity);
        btnSelectStartTime = findViewById(R.id.btn_select_start_time);
        btnSelectTime = findViewById(R.id.btn_select_time);
        btnDeploy = findViewById(R.id.btn_deploy);
        containerTechnical = findViewById(R.id.container_technical);
        spinnerIndicator = findViewById(R.id.spinner_indicator);
        spinnerOperator = findViewById(R.id.spinner_operator);
        containerDirection = findViewById(R.id.container_direction);
        spinnerDirection = findViewById(R.id.spinner_direction);
        btnSelectStartTime.setOnClickListener(v -> showDatePicker(true));
        btnSelectTime.setOnClickListener(v -> showDatePicker(false));
        btnDeploy.setOnClickListener(v -> attemptShowSummary());

        List<String> indicators = Arrays.asList("RSI (14)", "MACD (12,26,9)", "KDJ (9,3,3)", "BOLL (20,2)");
        ArrayAdapter<String> adapterInd = new ArrayAdapter<>(this, android.R.layout.simple_spinner_item, indicators);
        adapterInd.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
        spinnerIndicator.setAdapter(adapterInd);
        updateOperatorSpinner(false);

        List<String> directions = Arrays.asList("上涨 (Price Up)", "下跌 (Price Down)", "持平 (Flat/Range)");
        ArrayAdapter<String> adapterDir = new ArrayAdapter<>(this, android.R.layout.simple_spinner_item, directions);
        adapterDir.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
        spinnerDirection.setAdapter(adapterDir);
    }

    private void updateOperatorSpinner(boolean isVolume) {
        List<String> operators = isVolume ? Arrays.asList("大于 (Above)", "小于 (Below)", "等于 (Equal To)") : Arrays.asList("大于 (Above)", "小于 (Below)", "交叉向上 (Cross Up)", "交叉向下 (Cross Down)");
        ArrayAdapter<String> adapterOp = new ArrayAdapter<>(this, android.R.layout.simple_spinner_item, operators);
        adapterOp.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
        spinnerOperator.setAdapter(adapterOp);
    }

    private void setupTemplateUI() {
        tvTemplateName.setText(templateTitle);
        containerTechnical.setVisibility(View.GONE);
        containerDirection.setVisibility(View.GONE);
        etParam1.setVisibility(View.VISIBLE);
        switch (templateType) {
            case "TYPE_PRICE":
                tvTemplateDetail.setText("对比两个时间点的黄金价格。");
                etParam1.setVisibility(View.GONE); containerDirection.setVisibility(View.VISIBLE);
                break;
            case "TYPE_VOLATILITY": tvTemplateDetail.setText("博弈设定周期内的价格剧烈程度。"); etParam1.setHint("波动率门槛 (%)"); break;
            case "TYPE_VOLUME":
                tvTemplateDetail.setText("博弈指定交易日的成交总量。");
                containerTechnical.setVisibility(View.VISIBLE); findViewById(R.id.spinner_indicator).setVisibility(View.GONE);
                updateOperatorSpinner(true); etParam1.setHint("目标成交量 (吨)"); btnSelectStartTime.setVisibility(View.GONE); btnSelectTime.setText("选择交易日: 未选择");
                break;
            case "TYPE_TECHNICAL":
                tvTemplateDetail.setText("博弈特定技术指标是否达到设定形态。");
                containerTechnical.setVisibility(View.VISIBLE); findViewById(R.id.spinner_indicator).setVisibility(View.VISIBLE);
                updateOperatorSpinner(false); etParam1.setHint("触发数值 (如: 70)");
                break;
            case "TYPE_TOUCH": tvTemplateDetail.setText("极值触碰博弈。"); etParam1.setHint("触碰价格 (USD)"); break;
            case "TYPE_RELATIVE": tvTemplateDetail.setText("博弈黄金相对于其他资产的收益率。"); etParam1.setHint("对比标的 (如: BTC)"); break;
        }
    }

    private void showDatePicker(boolean isStart) {
        Calendar now = Calendar.getInstance(), target = isStart ? startCalendar : endCalendar;
        DatePickerDialog dialog = new DatePickerDialog(this, (view, year, month, day) -> {
            target.set(Calendar.YEAR, year); target.set(Calendar.MONTH, month); target.set(Calendar.DAY_OF_MONTH, day);
            target.set(Calendar.HOUR_OF_DAY, isStart ? 0 : 23); target.set(Calendar.MINUTE, isStart ? 0 : 59); target.set(Calendar.SECOND, 0);
            if (isStart) { startSelected = true; btnSelectStartTime.setText("开始: " + dateFormat.format(target.getTime())); }
            else { endSelected = true; btnSelectTime.setText("截止: " + dateFormat.format(target.getTime())); }
        }, now.get(Calendar.YEAR), now.get(Calendar.MONTH), now.get(Calendar.DAY_OF_MONTH));
        dialog.getDatePicker().setMinDate(System.currentTimeMillis() - 1000);
        dialog.show();
    }

    private void attemptShowSummary() {
        String p1 = etParam1.getText().toString().trim();
        if (p1.isEmpty() && !templateType.equals("TYPE_PRICE")) { Toast.makeText(this, "请输入定制参数", Toast.LENGTH_SHORT).show(); return; }
        if (!endSelected) { Toast.makeText(this, "请选择日期", Toast.LENGTH_SHORT).show(); return; }
        long now = System.currentTimeMillis(), start = startSelected ? startCalendar.getTimeInMillis() : now, end = endCalendar.getTimeInMillis();
        if (end <= start) { Toast.makeText(this, "截止日期必须晚于开始日期", Toast.LENGTH_SHORT).show(); return; }
        String cond = generateConditionString(p1), title = generateDescriptiveTitle(p1);
        long dur = (end - now) / 1000;
        Log.d("attemptShowSummary","开始时间"+String.valueOf(start));
        Log.d("attemptShowSummary","结束时间"+String.valueOf(end));
        Log.d("attemptShowSummary","博弈池持续时间"+String.valueOf(dur));

        if (dur <= 0) { Toast.makeText(this, "截止时间必须晚于当前", Toast.LENGTH_SHORT).show(); return; }
        showSummaryDialog(title, cond, start, end, dur);
    }

    private void showSummaryDialog(String title, String condition, long start, long end, long dur) {
        String liqStr = etInitialLiquidity.getText().toString().trim();
        if (liqStr.isEmpty()) liqStr = "100";
        final java.math.BigInteger liqWei = GoldMarketRepository.parseTokenAmountToWei(liqStr);
        AlertDialog.Builder builder = new AlertDialog.Builder(this);
        View v = LayoutInflater.from(this).inflate(R.layout.dialog_gold_pool_summary, null);
        builder.setView(v);
        ((TextView) v.findViewById(R.id.tv_summary_id)).setText("待部署: " + title);
        ((TextView) v.findViewById(R.id.tv_summary_logic)).setText(condition + "\n(初始: " + liqStr + " BKC)");
        ((TextView) v.findViewById(R.id.tv_summary_period)).setText((startSelected ? dateFormat.format(new Date(start)) : "当前") + " 至 " + dateFormat.format(new Date(end)));
        ((TextView) v.findViewById(R.id.tv_summary_creator)).setText(viewModel.getWalletAddress());
        ((TextView) v.findViewById(R.id.tv_summary_time)).setText(dateFormat.format(new Date()));
        Button btn = v.findViewById(R.id.btn_summary_close);
        btn.setText("确认并部署");
        AlertDialog dialog = builder.create();
        btn.setOnClickListener(view -> { dialog.dismiss(); viewModel.createGame(title, condition, "", "Premium", Arrays.asList("达成 (YES)", "未达成 (NO)"), dur, liqWei); });
        dialog.show();
    }


    private String generateDescriptiveTitle(String p1) {
        String startStr = dateFormat.format(startCalendar.getTime()), endStr = dateFormat.format(endCalendar.getTime());
        switch (templateType) {
            case "TYPE_PRICE": return String.format("%s 至 %s 黄金价格 %s", startStr, endStr, spinnerDirection.getSelectedItem().toString().split(" ")[0]);
            case "TYPE_VOLATILITY": return String.format("%s 前黄金波幅超过 %s%%", endStr, p1);
            case "TYPE_VOLUME": return String.format("%s 当日成交量 %s %s 吨", endStr, spinnerOperator.getSelectedItem().toString().split(" ")[0], p1);
            case "TYPE_TOUCH": return String.format("周期内金价触及 %s USD", p1);
            case "TYPE_TECHNICAL": return String.format("指标 %s 触发 %s %s", spinnerIndicator.getSelectedItem(), spinnerOperator.getSelectedItem(), p1);
            case "TYPE_RELATIVE": return String.format("黄金收益率跑赢 %s", p1);
            default: return templateTitle;
        }
    }

    private String generateConditionString(String p1) {
        String period = (startSelected ? "从 " + dateFormat.format(startCalendar.getTime()) + " 到 " : "截至 ") + dateFormat.format(endCalendar.getTime());
        switch (templateType) {
            case "TYPE_PRICE": return String.format("黄金价格在 %s 相对基准 %s", period, spinnerDirection.getSelectedItem());
            case "TYPE_VOLATILITY": return String.format("周期内波幅 >= %s%% (%s)", p1, period);
            case "TYPE_VOLUME": return String.format("指定日成交量 %s %s 吨 (%s)", spinnerOperator.getSelectedItem(), p1, dateFormat.format(endCalendar.getTime()));
            case "TYPE_TECHNICAL": return String.format("指标 %s %s %s (%s)", spinnerIndicator.getSelectedItem(), spinnerOperator.getSelectedItem(), p1, period);
            case "TYPE_TOUCH": return String.format("金价曾触及 %s USD (%s)", p1, period);
            case "TYPE_RELATIVE": return String.format("黄金收益率跑赢 %s (%s)", p1, period);
            default: return "自定义: " + p1;
        }
    }

}
