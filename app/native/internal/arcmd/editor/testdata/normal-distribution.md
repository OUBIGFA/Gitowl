**2. 联合密度函数（矩阵形式）：**

令向量 $\boldsymbol{Z} = \begin{pmatrix} X \\ Y \end{pmatrix}$，均值 $\boldsymbol{\mu} = \begin{pmatrix} \mu_X \\ \mu_Y \end{pmatrix}$，协方差矩阵 $\Sigma = \begin{pmatrix} \sigma_X^2 & \rho\sigma_X\sigma_Y \\ \rho\sigma_X\sigma_Y & \sigma_Y^2 \end{pmatrix}$。

$$
p(\boldsymbol{z}) = \frac{1}{2\pi |\Sigma|^{1/2}} \exp\left\{ -\frac{1}{2} (\boldsymbol{z}-\boldsymbol{\mu})^\top \Sigma^{-1} (\boldsymbol{z}-\boldsymbol{\mu}) \right\}
$$
