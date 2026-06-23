package com.example.brokerfi.xc.agent.gold.view;

import android.content.Intent;
import android.graphics.Color;
import android.graphics.Typeface;
import android.os.Bundle;
import android.text.Spannable;
import android.text.SpannableStringBuilder;
import android.text.style.AbsoluteSizeSpan;
import android.text.style.ForegroundColorSpan;
import android.text.style.StyleSpan;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.fragment.app.Fragment;
import androidx.lifecycle.ViewModelProvider;
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout;

import com.example.brokerfi.R;
import com.example.brokerfi.xc.agent.ai.DeepSeekClient;
import com.example.brokerfi.xc.agent.gold.model.data.GoldMarketRepository;
import com.example.brokerfi.xc.agent.gold.model.data.PinataClient;
import com.example.brokerfi.xc.agent.gold.model.logic.GoldAdvisoryManager;
import com.example.brokerfi.xc.agent.gold.viewmodel.GoldMarketViewModel;
import com.bumptech.glide.Glide;

import android.widget.ImageView;
import java.math.BigInteger;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Locale;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public class GoldMarketListFragment extends Fragment {
    private GoldMarketViewModel viewModel;
    private final List<GoldMarketRepository.GameModel> availableGames = new ArrayList<>();

    private LinearLayout marketListContainer;
    private SwipeRefreshLayout swipeRefresh;

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container, @Nullable Bundle savedInstanceState) {
        View view = inflater.inflate(R.layout.fragment_gold_market_list, container, false);
        initViews(view);
        return view;
    }

    @Override
    public void onViewCreated(@NonNull View view, @Nullable Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);
        viewModel = new ViewModelProvider(this).get(GoldMarketViewModel.class);
        observeViewModel();
        viewModel.loadData();
    }

    private void initViews(View v) {
        marketListContainer = v.findViewById(R.id.market_list_container);
        swipeRefresh = v.findViewById(R.id.swipe_refresh);
        swipeRefresh.setOnRefreshListener(() -> viewModel.loadData());
    }

    private void observeViewModel() {
        viewModel.getAvailableGames().observe(getViewLifecycleOwner(), games -> {
            availableGames.clear();
            if (games != null) {
                availableGames.addAll(games);
                Collections.sort(availableGames, (g1, g2) -> Integer.compare(g2.id, g1.id));
            }
            renderMarketList();
        });

        viewModel.getIsLoading().observe(getViewLifecycleOwner(), loading -> swipeRefresh.setRefreshing(loading));

        viewModel.getError().observe(getViewLifecycleOwner(), err -> {
            if (err != null) Toast.makeText(requireContext(), "Error: " + err, Toast.LENGTH_SHORT).show();
        });
    }

    private void renderMarketList() {
        marketListContainer.removeAllViews();
        LayoutInflater inflater = LayoutInflater.from(requireContext());
        for (GoldMarketRepository.GameModel game : availableGames) {
            View card = inflater.inflate(R.layout.item_gold_market_card, marketListContainer, false);
            TextView tvTitle = card.findViewById(R.id.tv_market_title);
            ImageView ivIcon = card.findViewById(R.id.iv_market_icon);
            String rawTitle = game.desc != null && !game.desc.isEmpty() ? game.desc : "博弈池 #" + game.id;
            tvTitle.setText(styleMarketTitle(rawTitle));

            if (game.avatarUrl != null && !game.avatarUrl.isEmpty()) {
                Glide.with(this).load(PinataClient.IPFS_GATEWAY + game.avatarUrl).placeholder(R.drawable.apartment_icon).into(ivIcon);
            } else {
                ivIcon.setImageResource(R.drawable.apartment_icon);
            }

            ((TextView) card.findViewById(R.id.tv_total_pool)).setText(GoldNoteMarketActivity.formatBkc(game.totalPool) + " BKC");
            long remaining = GoldNoteMarketActivity.remainingSecondsUntilDeadline(game.deadlineSec, System.currentTimeMillis());
            
            String status;
            int statusColor;
            if (game.isRefunded) {
                status = "已退款";
                statusColor = Color.GRAY;
            } else if (game.isResolved) {
                status = "已结算";
                statusColor = 0xFF3B82F6; // Blue
            } else if (remaining > 0) {
                status = "进行中";
                statusColor = 0xFF047857; // Green
            } else {
                status = "等待开奖";
                statusColor = Color.RED;
            }

            TextView tvStatus = card.findViewById(R.id.tv_market_status);
            tvStatus.setText(status);
            tvStatus.setTextColor(statusColor);
            ((TextView) card.findViewById(R.id.tv_deadline)).setText(GoldNoteMarketActivity.formatRemainingTime(remaining));

            if (game.virtualReserves != null && game.virtualReserves.size() >= 2) {
                BigInteger res0 = game.virtualReserves.get(0);
                BigInteger res1 = game.virtualReserves.get(1);
                BigInteger total = res0.add(res1);
                if (total.compareTo(BigInteger.ZERO) > 0) {
                    float yesRatio = (float) (res0.doubleValue() / total.doubleValue() * 100);
                    float noRatio = 100 - yesRatio;
                    View barYes = card.findViewById(R.id.bar_yes);
                    View barNo = card.findViewById(R.id.bar_no);
                    LinearLayout.LayoutParams lpYes = (LinearLayout.LayoutParams) barYes.getLayoutParams();
                    lpYes.weight = yesRatio;
                    barYes.setLayoutParams(lpYes);
                    LinearLayout.LayoutParams lpNo = (LinearLayout.LayoutParams) barNo.getLayoutParams();
                    lpNo.weight = noRatio;
                    barNo.setLayoutParams(lpNo);
                    ((TextView) card.findViewById(R.id.tv_yes_pct)).setText(String.format(Locale.getDefault(), "YES %.1f%%", yesRatio));
                    ((TextView) card.findViewById(R.id.tv_no_pct)).setText(String.format(Locale.getDefault(), "%.1f%% NO", noRatio));
                }
            }
            card.setOnClickListener(v -> {
                Intent intent = new Intent(requireContext(), GoldMarketDetailActivity.class);
                intent.putExtra("GAME_ID", game.id);
                startActivity(intent);
            });
            marketListContainer.addView(card);
        }
    }

    private SpannableStringBuilder styleMarketTitle(String title) {
        SpannableStringBuilder ssb = new SpannableStringBuilder(title);
        Pattern datePattern = Pattern.compile("\\d{4}-\\d{2}-\\d{2}");
        Matcher matcher = datePattern.matcher(title);
        while (matcher.find()) {
            ssb.setSpan(new ForegroundColorSpan(0xFF888888), matcher.start(), matcher.end(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
            ssb.setSpan(new AbsoluteSizeSpan(12, true), matcher.start(), matcher.end(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
        }
        String[] subjects = {"黄金价格", "黄金波幅", "成交量", "指标", "金价", "黄金收益率"};
        for (String sub : subjects) {
            int start = title.indexOf(sub);
            if (start >= 0) {
                ssb.setSpan(new ForegroundColorSpan(Color.BLACK), start, start + sub.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
                ssb.setSpan(new StyleSpan(Typeface.BOLD), start, start + sub.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
                ssb.setSpan(new AbsoluteSizeSpan(14, true), start, start + sub.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
            }
        }
        String[] ups = {"上涨", "剧烈", "高于", "跑赢", "Touched", "触及", "达标", "YES", "Price Up"};
        for (String kw : ups) {
            int start = title.indexOf(kw);
            if (start >= 0) {
                ssb.setSpan(new ForegroundColorSpan(0xFF047857), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
                ssb.setSpan(new StyleSpan(Typeface.BOLD), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
                ssb.setSpan(new AbsoluteSizeSpan(16, true), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
            }
        }
        String[] downs = {"下跌", "Price Down", "平稳", "低于", "跑输", "NO", "未达标"};
        for (String kw : downs) {
            int start = title.indexOf(kw);
            if (start >= 0) {
                ssb.setSpan(new ForegroundColorSpan(Color.RED), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
                ssb.setSpan(new StyleSpan(Typeface.BOLD), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
                ssb.setSpan(new AbsoluteSizeSpan(16, true), start, start + kw.length(), Spannable.SPAN_EXCLUSIVE_EXCLUSIVE);
            }
        }
        return ssb;
    }
}
