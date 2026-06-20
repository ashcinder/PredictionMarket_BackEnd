package com.example.brokerfi.xc.agent.gold.view;

import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.appcompat.app.AppCompatActivity;
import androidx.fragment.app.Fragment;
import androidx.lifecycle.ViewModelProvider;
import androidx.viewpager2.adapter.FragmentStateAdapter;
import androidx.viewpager2.widget.ViewPager2;

import com.example.brokerfi.R;
import com.example.brokerfi.xc.StorageUtil;
import com.example.brokerfi.xc.agent.ai.DeepSeekClient;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;
import com.example.brokerfi.xc.agent.gold.viewmodel.GoldNoteMarketViewModel;
import com.google.android.material.tabs.TabLayout;
import com.google.android.material.tabs.TabLayoutMediator;

import java.math.BigDecimal;
import java.math.BigInteger;
import java.math.RoundingMode;
import java.util.Locale;

public class GoldNoteMarketActivity extends AppCompatActivity {
    private static final BigDecimal DISPLAY_TOKEN_UNIT = new BigDecimal("1000000000000000000");
    private GoldNoteMarketViewModel viewModel;
    private TextView tvGoldPrice, tvGoldChange, tvGoldQuoteMeta;
    private boolean destroyed = false;
    private final Handler timerHandler = new Handler(Looper.getMainLooper());
    private final Runnable priceRefreshRunnable = new Runnable() {
        @Override public void run() {
            if (destroyed) return;
            viewModel.loadPrice();
            timerHandler.postDelayed(this, 60_000);
        }
    };

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_gold_note_market);
        destroyed = false;
        DeepSeekClient.init(this);
        if (StorageUtil.getCurrentPrivatekey(this) == null) { Toast.makeText(this, "请先登录", Toast.LENGTH_SHORT).show(); finish(); return; }
        viewModel = new ViewModelProvider(this).get(GoldNoteMarketViewModel.class);
        initViews();
        observeViewModel();
        viewModel.loadPrice();
        timerHandler.post(priceRefreshRunnable);
    }

    private void observeViewModel() {
        viewModel.getQuote().observe(this, quote -> { if (quote != null) updateGoldPriceUI(quote); });
        viewModel.getError().observe(this, err -> { if (err != null) { tvGoldPrice.setText("XAU $---.--"); tvGoldChange.setText("--.--%"); } });
    }

    @Override
    protected void onDestroy() {
        destroyed = true;
        timerHandler.removeCallbacks(priceRefreshRunnable);
        super.onDestroy();
    }

    private void initViews() {
        findViewById(R.id.btn_back).setOnClickListener(v -> finish());
        tvGoldPrice = findViewById(R.id.tv_gold_price);
        tvGoldChange = findViewById(R.id.tv_gold_change);
        tvGoldQuoteMeta = findViewById(R.id.tv_gold_quote_meta);
        TabLayout tabLayout = findViewById(R.id.tab_layout);
        ViewPager2 viewPager = findViewById(R.id.view_pager);
        viewPager.setAdapter(new FragmentStateAdapter(this) {
            @NonNull @Override public Fragment createFragment(int position) {
                switch (position) {
                    case 0: return new GoldMarketListFragment();
                    case 1: return new GoldMyPositionsFragment();
                    case 2: return new GoldCreatePoolFragment();
                    case 3: return new AIChatFragment();
                    default: return new GoldMarketListFragment();
                }
            }
            @Override public int getItemCount() { return 4; }
        });
        new TabLayoutMediator(tabLayout, viewPager, (tab, position) -> {
            switch (position) {
                case 0: tab.setText("博弈市场"); break;
                case 1: tab.setText("个人持仓"); break;
                case 2: tab.setText("创建博弈"); break;
                case 3: tab.setText("AI投研"); break;
            }
        }).attach();
    }

    private void updateGoldPriceUI(GoldAdvisoryManager.Advisory quote) {
        tvGoldPrice.setText(String.format(Locale.getDefault(), "XAU $%,.2f/oz", quote.priceUsd));
        tvGoldChange.setText(String.format(Locale.getDefault(), "%+.2f%%", quote.change24h));
        tvGoldQuoteMeta.setText("来源 " + quote.quoteSource + " · " + quote.quoteUpdatedAt);
    }

    public static String formatShareAmount(BigInteger value) {
        if (value == null || value.compareTo(BigInteger.ZERO) <= 0) return "0";
        BigDecimal shares = new BigDecimal(value).divide(DISPLAY_TOKEN_UNIT, 18, RoundingMode.DOWN);
        if (shares.compareTo(BigDecimal.ZERO) > 0 && shares.compareTo(new BigDecimal("0.000001")) < 0) return "<0.000001";
        return shares.setScale(6, RoundingMode.HALF_UP).stripTrailingZeros().toPlainString();
    }

    public static String formatRemainingTime(long rem) {
        if (rem <= 0) return "已截止";
        long days = rem / 86400;
        return days >= 1 ? String.format(Locale.getDefault(), "距结束 %d 天", days) : "距结束不足 1 天";
    }

    public static String formatBkc(BigInteger value) {
        if (value == null) return "0.00";
        BigDecimal bkc = new BigDecimal(value).divide(DISPLAY_TOKEN_UNIT, 2, RoundingMode.HALF_UP);
        return String.format(Locale.getDefault(), "%,.2f", bkc.doubleValue());
    }

    public static long remainingSecondsUntilDeadline(long raw, long now) {
        if (raw <= 0) return 0;
        long deadline = (raw > 10000000000L) ? raw : raw * 1000;
        long diff = deadline - now;
        return diff > 0 ? (diff / 1000) : 0;
    }
}
