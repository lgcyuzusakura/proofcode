package dev.proofcode.control.provider;

import java.math.BigDecimal;
import java.util.*;

/** User-defined limits and whole-request price tiers; stored in the task snapshot. */
public record ModelSettings(Integer contextTokens, Integer maxInputTokens, Integer maxOutputTokens,
                            String compression, String currency, List<PriceTier> prices) {
    public record PriceTier(int upToInputTokens, BigDecimal input, BigDecimal output,
                            BigDecimal cacheRead, BigDecimal cacheWrite) {}
    public static ModelSettings defaults() { return new ModelSettings(128000,16000,2048,"balanced","CNY",List.of()); }
    public ModelSettings validated() {
        int contextTokens=this.contextTokens==null?128000:this.contextTokens;
        int maxInputTokens=this.maxInputTokens==null?16000:this.maxInputTokens;
        int maxOutputTokens=this.maxOutputTokens==null?2048:this.maxOutputTokens;
        String compression=this.compression==null||this.compression.isBlank()?"balanced":this.compression;
        String currency=this.currency==null||this.currency.isBlank()?"CNY":this.currency;
        if(contextTokens<1024||contextTokens>2000000||maxInputTokens<256||maxOutputTokens<1
           ||(long)maxInputTokens+maxOutputTokens+256>contextTokens)
            throw new IllegalArgumentException("上下文需为 1,024–2,000,000 Token；输入上限 + 输出上限 + 256 需小于等于上下文");
        if(!Set.of("gentle","balanced","aggressive").contains(compression))
            throw new IllegalArgumentException("请选择轻度、均衡或强力压缩");
        if(!currency.matches("[A-Z]{3}"))throw new IllegalArgumentException("币种使用三位大写代码，如 CNY 或 USD");
        var rows=prices==null?List.<PriceTier>of():prices;
        if(rows.size()>32)throw new IllegalArgumentException("价格区间最多 32 档");
        int previous=0;
        for(int i=0;i<rows.size();i++) {
            var row=rows.get(i);
            if(row==null||row.upToInputTokens()<0||row.upToInputTokens()>2000000
               ||(row.upToInputTokens()==0&&i!=rows.size()-1)
               ||(row.upToInputTokens()!=0&&row.upToInputTokens()<=previous))
                throw new IllegalArgumentException("价格区间上限必须递增；仅最后一档可不设上限");
            price(row.input(),false);price(row.output(),false);price(row.cacheRead(),true);price(row.cacheWrite(),true);
            previous=row.upToInputTokens();
        }
        return new ModelSettings(contextTokens,maxInputTokens,maxOutputTokens,compression,currency,List.copyOf(rows));
    }
    private static void price(BigDecimal value,boolean optional) {
        if(value==null&&optional)return;
        if(value==null||value.signum()<0||value.compareTo(new BigDecimal("1000000"))>0||value.scale()>12)
            throw new IllegalArgumentException("每百万 Token 单价需为 0–1,000,000，最多 12 位小数");
    }
    public static Map<String,ModelSettings> validate(Map<String,ModelSettings> values) {
        if(values.size()>1000)throw new IllegalArgumentException("模型配置过多");
        var result=new TreeMap<String,ModelSettings>();
        values.forEach((key,value)->{
            if(key==null||key.isBlank()||key.length()>200||value==null)throw new IllegalArgumentException("模型配置无效");
            result.put(key,value.validated());
        });
        return result;
    }
}
