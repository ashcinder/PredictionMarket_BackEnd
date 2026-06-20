package com.example.brokerfi.xc.agent.gold.view;

import android.content.Intent;
import android.os.Bundle;
import android.view.LayoutInflater;
import android.view.View;
import android.view.ViewGroup;
import android.widget.GridLayout;
import android.widget.ImageView;
import android.widget.TextView;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.fragment.app.Fragment;

import com.example.brokerfi.R;

public class GoldCreatePoolFragment extends Fragment {

    @Nullable
    @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container, @Nullable Bundle savedInstanceState) {
        View view = inflater.inflate(R.layout.fragment_gold_create_pool, container, false);
        initTemplates(view.findViewById(R.id.template_grid));
        return view;
    }

    private void initTemplates(GridLayout grid) {
        addTemplate(grid, "价格涨跌", "预测金价在某日涨跌", R.drawable.ic_template_price, "TYPE_PRICE");
        addTemplate(grid, "波动幅度", "预测行情剧烈程度", R.drawable.ic_template_volatility, "TYPE_VOLATILITY");
        addTemplate(grid, "交易量", "预测市场整体流动性", R.drawable.ic_template_volume, "TYPE_VOLUME");
        addTemplate(grid, "技术指标", "预测 RSI/MACD 形态", R.drawable.ic_template_technical, "TYPE_TECHNICAL");
        addTemplate(grid, "极值触碰", "预测金价是否触及目标", R.drawable.ic_template_touch, "TYPE_TOUCH");
        addTemplate(grid, "跑赢率", "黄金 vs BTC 收益率", R.drawable.ic_template_relative, "TYPE_RELATIVE");
    }

    private void addTemplate(GridLayout grid, String title, String desc, int iconRes, String type) {
        View card = LayoutInflater.from(requireContext()).inflate(R.layout.item_gold_template_card, grid, false);
        ((TextView) card.findViewById(R.id.tv_template_title)).setText(title);
        ((TextView) card.findViewById(R.id.tv_template_desc)).setText(desc);
        ((ImageView) card.findViewById(R.id.iv_template_icon)).setImageResource(iconRes);
        
        GridLayout.LayoutParams params = new GridLayout.LayoutParams();
        params.width = 0;
        params.height = ViewGroup.LayoutParams.WRAP_CONTENT;
        params.columnSpec = GridLayout.spec(GridLayout.UNDEFINED, 1f);
        card.setLayoutParams(params);
        
        card.setOnClickListener(v -> {
            Intent intent = new Intent(requireContext(), GoldCreateCustomActivity.class);
            intent.putExtra("TEMPLATE_TYPE", type);
            intent.putExtra("TEMPLATE_TITLE", title);
            startActivity(intent);
        });
        grid.addView(card);
    }
}
