package com.example.brokerfi.xc.agent.gold.view;

import android.app.AlertDialog;
import android.app.DatePickerDialog;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.net.Uri;
import android.os.Bundle;
import android.provider.MediaStore;
import android.view.LayoutInflater;
import android.view.View;
import android.widget.ArrayAdapter;
import android.widget.Button;
import android.widget.EditText;
import android.widget.ImageView;
import android.widget.LinearLayout;
import android.widget.Spinner;
import android.widget.TextView;
import android.widget.Toast;
import android.util.Log;

import androidx.activity.result.ActivityResultLauncher;
import androidx.activity.result.contract.ActivityResultContracts;
import androidx.appcompat.app.AppCompatActivity;
import androidx.lifecycle.ViewModelProvider;

import com.bumptech.glide.Glide;
import com.example.brokerfi.R;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.viewmodel.GoldCreatePoolViewModel;

import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
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
    private Button btnSelectStartTime, btnSelectTime, btnDeploy, btnSelectImage;
    private ImageView ivPoolIcon;
    private LinearLayout containerTechnical, containerDirection;
    private Spinner spinnerIndicator, spinnerOperator, spinnerDirection;
    private SimpleDateFormat dateFormat = new SimpleDateFormat("yyyy-MM-dd", Locale.getDefault());
    
    private byte[] selectedImageData = null;
    private final ActivityResultLauncher<Intent> imagePickerLauncher = registerForActivityResult(
            new ActivityResultContracts.StartActivityForResult(),
            result -> {
                if (result.getResultCode() == RESULT_OK && result.getData() != null) {
                    handleImageResult(result.getData().getData());
                }
            });

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_gold_create_custom);
        templateType = getIntent().getStringExtra("TEMPLATE_TYPE");
        templateTitle = getIntent().getStringExtra("TEMPLATE_TITLE");
        viewModel = new ViewModelProvider(this).get(GoldCreatePoolViewModel.class);
        initViews();
        setupTemplateUI();
        
        // Handle AI Parsed Data if available
        String aiData = getIntent().getStringExtra("AI_PARSED_DATA");
        if (aiData != null) {
            applyAiParsedData(aiData);
        }
        
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
                if (status.startsWith("Success") || status.equals("Success")) finish();
            }
        });
        viewModel.getError().observe(this, err -> {
            if (err != null) {
                new AlertDialog.Builder(this)
                        .setTitle("博弈池创建失败")
                        .setMessage(err)
                        .setPositiveButton("知道了", null)
                        .show();
            }
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
        btnSelectImage = findViewById(R.id.btn_select_image);
        ivPoolIcon = findViewById(R.id.iv_pool_icon);
        btnDeploy = findViewById(R.id.btn_deploy);
        containerTechnical = findViewById(R.id.container_technical);
        spinnerIndicator = findViewById(R.id.spinner_indicator);
        spinnerOperator = findViewById(R.id.spinner_operator);
        containerDirection = findViewById(R.id.container_direction);
        spinnerDirection = findViewById(R.id.spinner_direction);
        
        btnSelectStartTime.setOnClickListener(v -> showDatePicker(true));
        btnSelectTime.setOnClickListener(v -> showDatePicker(false));
        btnSelectImage.setOnClickListener(v -> pickImage());
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
        switch (templateType != null ? templateType : "") {
            case "TYPE_PRICE":
                tvTemplateDetail.setText("对比两个时间点的黄金价格。");
                etParam1.setVisibility(View.GONE); containerDirection.setVisibility(View.VISIBLE);
                break;
            case "TYPE_VOLATILITY": tvTemplateDetail.setText("博弈设定周期内的价格剧烈程度。"); etParam1.setHint("波动率门槛 (%)"); break;
            case "TYPE_VOLUME":
                tvTemplateDetail.setText("博弈指定交易日的成交总量。");
                containerTechnical.setVisibility(View.VISIBLE); spinnerIndicator.setVisibility(View.GONE);
                updateOperatorSpinner(true); etParam1.setHint("目标成交量 (吨)"); btnSelectStartTime.setVisibility(View.GONE); btnSelectTime.setText("选择交易日: 未选择");
                break;
            case "TYPE_TECHNICAL":
                tvTemplateDetail.setText("博弈特定技术指标是否达到设定形态。");
                containerTechnical.setVisibility(View.VISIBLE); spinnerIndicator.setVisibility(View.VISIBLE);
                updateOperatorSpinner(false); etParam1.setHint("触发数值 (如: 70)");
                break;
            case "TYPE_TOUCH": tvTemplateDetail.setText("极值触碰博弈。"); etParam1.setHint("触碰价格 (USD)"); break;
            case "TYPE_RELATIVE": tvTemplateDetail.setText("博弈黄金相对于其他资产的收益率。"); etParam1.setHint("对比标的 (如: BTC)"); break;
            case "TYPE_PRICE_THRESHOLD":
                tvTemplateDetail.setText("博弈截止时刻金价大于/小于/等于指定价格。");
                containerTechnical.setVisibility(View.VISIBLE); spinnerIndicator.setVisibility(View.GONE);
                updateOperatorSpinner(true); etParam1.setHint("目标价格 (USD)");
                break;
            case "TYPE_EVENT":
                tvTemplateDetail.setText("博弈指定宏观/财经事件在截止日期前是否发生。");
                etParam1.setHint("事件描述 (如: 美联储降息)");
                break;
        }
    }

    private void applyAiParsedData(String jsonStr) {
        try {
            JSONObject json = new JSONObject(jsonStr);
            // templateType already set from intent but can be overwritten
            templateType = json.optString("type", templateType);
            etParam1.setText(json.optString("param1", ""));
            spinnerDirection.setSelection(json.optInt("directionIdx", 0));
            spinnerIndicator.setSelection(json.optInt("indicatorIdx", 0));
            spinnerOperator.setSelection(json.optInt("operatorIdx", 0));
            etInitialLiquidity.setText(json.optString("liquidity", "1"));

            int days = json.optInt("daysFromNow", 7);
            endCalendar = Calendar.getInstance();
            endCalendar.add(Calendar.DAY_OF_YEAR, days);
            endSelected = true;
            btnSelectTime.setText("截止: " + dateFormat.format(endCalendar.getTime()));

            setupTemplateUI();
            Toast.makeText(this, "✨ 已应用 AI 智能解析规则", Toast.LENGTH_SHORT).show();
        } catch (Exception e) {
            Log.e("GoldCreate", "Failed to apply AI data", e);
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

    private void pickImage() {
        Intent intent = new Intent(Intent.ACTION_PICK, MediaStore.Images.Media.EXTERNAL_CONTENT_URI);
        imagePickerLauncher.launch(intent);
    }

    private void handleImageResult(Uri uri) {
        try {
            Glide.with(this).load(uri).into(ivPoolIcon);
            InputStream is = getContentResolver().openInputStream(uri);
            Bitmap bitmap = BitmapFactory.decodeStream(is);
            if (bitmap != null) {
                ByteArrayOutputStream baos = new ByteArrayOutputStream();
                bitmap.compress(Bitmap.CompressFormat.JPEG, 80, baos);
                selectedImageData = baos.toByteArray();
                Log.d("GoldCreate", "图片选取成功, 大小: " + selectedImageData.length);
            }
        } catch (Exception e) {
            Toast.makeText(this, "图片加载失败", Toast.LENGTH_SHORT).show();
        }
    }

    private void attemptShowSummary() {
        String p1 = etParam1.getText().toString().trim();
        if (p1.isEmpty() && !"TYPE_PRICE".equals(templateType)) { 
            Toast.makeText(this, "请输入定制参数", Toast.LENGTH_SHORT).show(); 
            return; 
        }
        if (!endSelected) { Toast.makeText(this, "请选择日期", Toast.LENGTH_SHORT).show(); return; }
        long now = System.currentTimeMillis(), start = startSelected ? startCalendar.getTimeInMillis() : now, end = endCalendar.getTimeInMillis();
        if (end <= start) { Toast.makeText(this, "截止日期必须晚于开始日期", Toast.LENGTH_SHORT).show(); return; }
        
        String cond = generateConditionString(p1), title = generateDescriptiveTitle(p1);
        long dur = (end - now) / 1000;

        if (dur <= 0) { Toast.makeText(this, "截止时间必须晚于当前", Toast.LENGTH_SHORT).show(); return; }
        showSummaryDialog(title, cond, start, end, dur);
    }

    private void showSummaryDialog(String title, String condition, long start, long end, long dur) {
        String liqStr = etInitialLiquidity.getText().toString().trim();
        if (liqStr.isEmpty()) liqStr = "1";
        final java.math.BigInteger liqWei = GoldMarketRepository.parseTokenAmountToWei(liqStr);
        if (liqWei == null) {
            Toast.makeText(this, "初始流动性金额无效，请输入有效数字（如 1）", Toast.LENGTH_SHORT).show();
            return;
        }
        
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
        btn.setOnClickListener(view -> { 
            dialog.dismiss(); 
            viewModel.createGame(title, condition, selectedImageData, "Premium", Arrays.asList("达成 (YES)", "未达成 (NO)"), dur, liqWei); 
        });
        dialog.show();
    }

    /**
     * 安全获取 spinner 当前选中文本的中文部分（"大于 (Above)" → "大于"）
     */
    private String safeOperatorText() {
        Object item = spinnerOperator.getSelectedItem();
        if (item == null) return "大于";
        String[] parts = item.toString().split(" ");
        return parts.length > 0 ? parts[0] : "大于";
    }

    private String generateDescriptiveTitle(String p1) {
        String startStr = dateFormat.format(startCalendar.getTime()), endStr = dateFormat.format(endCalendar.getTime());
        switch (templateType != null ? templateType : "") {
            case "TYPE_PRICE": return String.format("%s 至 %s 黄金价格 %s", startStr, endStr, spinnerDirection.getSelectedItem().toString().split(" ")[0]);
            case "TYPE_VOLATILITY": return String.format("%s 前黄金波幅超过 %s%%", endStr, p1);
            case "TYPE_VOLUME": return String.format("%s 当日成交量 %s %s 吨", endStr, safeOperatorText(), p1);
            case "TYPE_TOUCH": return String.format("周期内金价触及 %s USD", p1);
            case "TYPE_TECHNICAL": return String.format("指标 %s 触发 %s %s", spinnerIndicator.getSelectedItem(), spinnerOperator.getSelectedItem(), p1);
            case "TYPE_RELATIVE": return String.format("黄金收益率跑赢 %s", p1);
            case "TYPE_PRICE_THRESHOLD": return String.format("截止 %s 金价 %s %s USD", endStr, safeOperatorText(), p1);
            case "TYPE_EVENT": return String.format("「%s」是否发生", p1);
            default: return templateTitle;
        }
    }

    private String generateConditionString(String p1) {
        String period = (startSelected ? "从 " + dateFormat.format(startCalendar.getTime()) + " 到 " : "截至 ") + dateFormat.format(endCalendar.getTime());
        switch (templateType != null ? templateType : "") {
            case "TYPE_PRICE": return String.format("黄金价格在 %s 相对基准 %s", period, spinnerDirection.getSelectedItem());
            case "TYPE_VOLATILITY": return String.format("周期内波幅 >= %s%% (%s)", p1, period);
            case "TYPE_VOLUME": return String.format("指定日成交量 %s %s 吨 (%s)", spinnerOperator.getSelectedItem(), p1, dateFormat.format(endCalendar.getTime()));
            case "TYPE_TECHNICAL": return String.format("指标 %s %s %s (%s)", spinnerIndicator.getSelectedItem(), spinnerOperator.getSelectedItem(), p1, period);
            case "TYPE_TOUCH": return String.format("金价曾触及 %s USD (%s)", p1, period);
            case "TYPE_RELATIVE": return String.format("黄金收益率跑赢 %s (%s)", p1, period);
            case "TYPE_PRICE_THRESHOLD": return String.format("黄金价格 %s %s USD (%s)", safeOperatorText(), p1, period);
            case "TYPE_EVENT": return String.format("事件「%s」是否发生 (%s)", p1, period);
            default: return "自定义: " + p1;
        }
    }
}
